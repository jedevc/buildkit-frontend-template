package build

import (
	"context"

	"github.com/containerd/containerd/platforms"
	"github.com/jedevc/buildkit-frontend-template/internal/convert"
	"github.com/moby/buildkit/exporter/containerimage/image"
	"github.com/moby/buildkit/frontend/dockerui"
	"github.com/moby/buildkit/frontend/gateway/client"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
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
	bc, err := dockerui.NewClient(c)
	if err != nil {
		return nil, err
	}

	// extract the base image from the build args
	base := "ubuntu"
	if v, ok := bc.BuildArgs["base"]; ok {
		base = v
	}

	src, err := bc.ReadEntrypoint(ctx)
	if err != nil {
		return nil, err
	}
	opt := convert.ConvertOpt{
		Base: base,
		Src:  *src,
	}

	rb, err := bc.Build(ctx, func(ctx context.Context, platform *ocispecs.Platform, idx int) (client.Reference, *image.Image, error) {
		opt := opt
		opt.Platform = platforms.DefaultSpec()
		if platform != nil {
			opt.Platform = *platform
		}

		st, img, err := convert.Convert2LLB(ctx, c, opt)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to marshal LLB definition")
		}

		def, err := st.Marshal(ctx)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to marshal LLB definition")
		}

		r, err := c.Solve(ctx, client.SolveRequest{
			Definition:   def.ToPB(),
			CacheImports: bc.CacheImports,
		})
		if err != nil {
			return nil, nil, err
		}

		ref, err := r.SingleRef()
		if err != nil {
			return nil, nil, err
		}

		return ref, img, nil
	})
	if err != nil {
		return nil, err
	}

	return rb.Finalize()
}
