.PHONY: all build
	
all: build

build:
	docker buildx build . --output type=oci,dest=build/frontend,tar=false
	
deploy:
	test -n "$(DEST)"
	docker buildx build . -t $(DEST) --push --platform linux/amd64,linux/arm64
	
example:
	test -n "$(EXAMPLE)"
	docker buildx build examples/$(EXAMPLE) --build-context custom-frontend=oci-layout://./build/frontend -t example-$(EXAMPLE) --load
