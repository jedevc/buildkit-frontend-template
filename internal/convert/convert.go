package convert

import (
	"context"
	"encoding/json"

	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/exporter/containerimage/image"
	"github.com/moby/buildkit/frontend/dockerui"
	"github.com/moby/buildkit/frontend/gateway/client"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

type ConvertOpt struct {
	// Src is the source code to build
	Src dockerui.Source
	// Base is the base image to use
	Base string

	// Platform is the platform to build for
	Platform ocispecs.Platform
}

func Convert2LLB(ctx context.Context, c client.Client, opt ConvertOpt) (llb.State, *image.Image, error) {
	st, img, err := getBase(ctx, c, opt.Base, opt.Platform)
	if err != nil {
		return llb.State{}, nil, err
	}
	if opt.Base == "scratch" {
		return st, img, nil
	}

	st = st.File(llb.Mkdir("/source", 0755, llb.WithParents(true)))
	st = st.File(llb.Mkfile("/source/data", 0644, []byte(opt.Src.Data)))
	st = st.Run(llb.Args([]string{`sh`, `-c`, `date > /source/date`})).Root()

	return st, img, nil
}

func getBase(ctx context.Context, c client.Client, base string, platform ocispecs.Platform) (llb.State, *image.Image, error) {
	var img image.Image

	state := llb.Scratch()

	if base != "scratch" {
		state = llb.Image(base)
		ref, err := reference.ParseNormalizedNamed(base)
		if err != nil {
			return llb.State{}, nil, errors.Wrapf(err, "failed to parse stage name %q", base)
		}

		base := reference.TagNameOnly(ref).String()
		_, dt, err := c.ResolveImageConfig(ctx, base, llb.ResolveImageConfigOpt{
			Platform: &platform,
		})

		if err != nil {
			return llb.State{}, nil, errors.Wrap(err, "resolve image config")
		}

		if err := json.Unmarshal(dt, &img); err != nil {
			return llb.State{}, nil, errors.Wrap(err, "failed to parse image config")
		}

		state, err = state.WithImageConfig(dt)
		if err != nil {
			return llb.State{}, nil, err
		}
	}

	return state, &img, nil
}
