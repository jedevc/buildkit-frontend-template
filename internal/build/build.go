package build

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/containerd/containerd/platforms"
	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/util/system"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const (
	localNameDockerfile  = "dockerfile"
	keyFilename          = "filename"
	defaultBuildFileName = "clockerfile"
	buildArgPrefix       = "build-arg:"
	keyTargetPlatform    = "platform"
)

// Builder contains the Build function we pass to buildkit
type Builder struct {
}

// New creates a new builder with the appropriate converter
func New() Builder {
	return Builder{}
}

// Build runs the build
func (b Builder) Build(ctx context.Context, c client.Client) (*client.Result, error) {
	opts := c.BuildOpts().Opts

	clockerfile, err := getClockerfile(ctx, c)
	if err != nil {
		return nil, err
	}

	var targetPlatforms []ocispecs.Platform
	if v := opts[keyTargetPlatform]; v != "" {
		var err error
		targetPlatforms, err = parsePlatforms(v)
		if err != nil {
			return nil, err
		}
	}
	if targetPlatforms == nil {
		p := platforms.DefaultSpec()
		targetPlatforms = []ocispecs.Platform{p}
	}

	expPlatforms := &exptypes.Platforms{
		Platforms: make([]exptypes.Platform, len(targetPlatforms)),
	}

	res := client.NewResult()
	eg, ctx := errgroup.WithContext(ctx)
	for i, p := range targetPlatforms {
		i := i
		p := p

		eg.Go(func() error {
			st, img, err := getBase(ctx, c, "ubuntu", &p)
			if err != nil {
				return err
			}
			st = st.Run(llb.Args([]string{"sh", "-c", "apt-get update && apt-get install -y cloud-init"})).Root()

			def, err := st.Marshal(ctx)
			if err != nil {
				return errors.Wrapf(err, "failed to marshal build")
			}

			r, err := c.Solve(ctx, client.SolveRequest{
				Definition: def.ToPB(),
			})
			if err != nil {
				return errors.Wrap(err, "definition to pb")
			}

			config, err := json.Marshal(img)
			if err != nil {
				return errors.Wrap(err, "image config marshal")
			}

			ref, err := r.SingleRef()
			if err != nil {
				return errors.Wrap(err, "getting single ref")
			}

			k := platforms.Format(p)
			res.AddMeta(fmt.Sprintf("%s/%s", exptypes.ExporterImageConfigKey, k), config)
			res.AddRef(k, ref)
			expPlatforms.Platforms[i] = exptypes.Platform{
				ID:       k,
				Platform: p,
			}

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	dt, err := json.Marshal(expPlatforms)
	if err != nil {
		return nil, err
	}
	res.AddMeta(exptypes.ExporterPlatformsKey, dt)

	return res, nil
}

func getClockerfile(ctx context.Context, c client.Client) ([]byte, error) {
	opts := c.BuildOpts().Opts
	filename := opts[keyFilename]
	if filename == "" {
		filename = defaultBuildFileName
	}

	name := "load build definition"
	if filename != defaultBuildFileName {
		name += " from " + filename
	}

	src := llb.Local(localNameDockerfile,
		llb.SessionID(c.BuildOpts().SessionID),
		llb.WithCustomName("[internal] "+name),
	)

	def, err := src.Marshal(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal local source")
	}

	res, err := c.Solve(ctx, client.SolveRequest{
		Definition: def.ToPB(),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve build file")
	}

	ref, err := res.SingleRef()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get single ref")
	}

	data, err := ref.ReadFile(ctx, client.ReadRequest{
		Filename: filename,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read build file %q", data)
	}

	lines := bytes.Split(data, []byte("\n"))
	if bytes.HasPrefix(lines[0], []byte("#syntax=")) {
		data = data[len(lines[0]):]
	}
	data = bytes.TrimSpace(data)

	return data, nil
}

func parsePlatforms(v string) ([]ocispecs.Platform, error) {
	var pp []ocispecs.Platform
	for _, v := range strings.Split(v, ",") {
		p, err := platforms.Parse(v)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse target platform %s", v)
		}
		p = platforms.Normalize(p)
		pp = append(pp, p)
	}
	return pp, nil
}

func getBase(ctx context.Context, c client.Client, base string, platform *ocispecs.Platform) (llb.State, *ocispecs.Image, error) {
	img := &ocispecs.Image{
		Architecture: platform.Architecture,
		OS:           platform.OS,
		OSVersion:    platform.OSVersion,
		RootFS: ocispecs.RootFS{
			Type: "layers",
		},
		Config: ocispecs.ImageConfig{
			WorkingDir: "/",
			Env:        []string{"PATH=" + system.DefaultPathEnvUnix},
			Labels:     map[string]string{},
		},
	}

	state := llb.Scratch()

	if base != "scratch" {
		state = llb.Image(base)
		ref, err := reference.ParseNormalizedNamed(base)
		if err != nil {
			return llb.State{}, nil, errors.Wrapf(err, "failed to parse stage name %q", base)
		}

		base := reference.TagNameOnly(ref).String()
		_, dt, err := c.ResolveImageConfig(ctx, base, llb.ResolveImageConfigOpt{
			Platform: platform,
		})

		if err != nil {
			return llb.State{}, nil, errors.Wrap(err, "resolve image config")
		}

		var i ocispecs.Image
		if err := json.Unmarshal(dt, &i); err != nil {
			return llb.State{}, nil, errors.Wrap(err, "failed to parse image config")
		}

		img.Config = i.Config
		img.Config.Env = append([]string{}, i.Config.Env...)
		img.Config.Cmd = append([]string{}, i.Config.Cmd...)
		img.Config.Entrypoint = append([]string{}, i.Config.Entrypoint...)
		if img.Config.Labels == nil {
			img.Config.Labels = map[string]string{}
		}

		for _, env := range i.Config.Env {
			k, v := parseKeyValue(env)
			state = state.AddEnv(k, v)
		}
	}

	return state, img, nil
}

func parseKeyValue(env string) (string, string) {
	parts := strings.SplitN(env, "=", 2)
	v := ""
	if len(parts) > 1 {
		v = parts[1]
	}

	return parts[0], v
}
