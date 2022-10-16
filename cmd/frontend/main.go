package main

import (
	"github.com/moby/buildkit/frontend/gateway/grpcclient"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/sirupsen/logrus"

	"github.com/jedevc/frontend/internal/build"
)

func main() {
	builder := build.New()
	if err := grpcclient.RunFromEnvironment(appcontext.Context(), builder.Build); err != nil {
		logrus.Errorf("fatal error: %+v", err)
		panic(err)
	}
}
