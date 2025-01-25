export KUBECONFIG= $(HOME)/.kube/config

ifeq ($(shell uname), Darwin)
SED_IN_PLACE := sed -i ''
else
SED_IN_PLACE := sed -i
endif

BINARY_NAME=elika-proxy-srv

IMG ?= elika-proxy:latest

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be_cluster executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped cmd fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

PROTO_ROOT := proto
PROTO_FILES = $(shell find $(PROTO_ROOT) -name "*.proto")
PROTO_OUT := ./pkg

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
ENVTEST ?= $(LOCALBIN)/setup-envtest

.PHONY: all
all: build

.PHONY: fmt
fmt:
	gofmt -l -w -d  ./pkg/be_cluster ./pkg/common ./pkg/proxy ./pkg/respio ./cmd/proxy ./pkg/web_service

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: proto-generate
proto-generate:
	protoc \
	--go_out=$(PROTO_OUT) \
	--go-grpc_out=$(PROTO_OUT) \
	--go-grpc_opt=require_unimplemented_servers=false \
	--go_opt=paths=source_relative \
	--go-grpc_opt=paths=source_relative	\
	--experimental_allow_proto3_optional \
	$(PROTO_FILES)

.PHONY: build-prototype
build-prototype:
	cd prototype && go build -o ./bin/elika-proxy-prototype main.go

.PHONY: build
build: fmt
ifeq ($(PROXY_RUNTIME),prod)
	go build -ldflags="-s -w" -a -o bin/$(BINARY_NAME) cmd/proxy/main.go
else
	go build -o bin/$(BINARY_NAME) cmd/proxy/main.go
endif

.PHONY: test
test: build
	go test $(shell go list ./... | grep -v /proto | grep -v /cmd) -v -coverprofile cover.out

.PHONY: docker-build
docker-build: build ## Build docker image with the manager.
	docker build -f ./docker/Dockerfile -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

# PLATFORMS defines the target platforms for  the manager image be_cluster build to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - able to use docker buildx . More info: https://docs.docker.com/build/buildx/
# - have enable BuildKit, More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be_cluster able to push the image for your registry (i.e. if you do not inform a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To properly provided solutions that supports more than one platform you should use this option.
#PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
PLATFORMS ?= linux/arm64,linux/amd64
.PHONY: docker-buildx
docker-buildx: build ##Build and push docker image for the manager for cross-platform support
	#copy existing ./Dockerfile and insert --platform=${BUILDPLATFORM} into ./Dockerfile.cross, and preserve the original ./Dockerfile
    #sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' ./Dockerfile > ./Dockerfile.cross
	- docker buildx create --name elika-proxy-project-v3-builder
	docker buildx use elika-proxy-project-v3-builder
	- docker buildx build --push --platform=$(PLATFORMS)  --tag ${IMG} -f docker/Dockerfile .
	- docker buildx rm elika-proxy-project-v3-builder

.PHONY: run-local
run-local: envtest build fmt vet
	go run cmd/proxy/main.go $(ARGS)

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = true
endif

.PHONY: rbac
rbac: kustomize
	$(KUSTOMIZE) build config/rbac | kubectl apply -f -

SRC_DIR=config/rbac
DST_DIR=tmp/rbac
NAMESPACE=$(SRC_DIR)/namespace.yaml
MODIFIED_NAMESPACE=$(DST_DIR)/namespace.yaml
SERVICE_ACCOUNT=$(SRC_DIR)/service_account.yaml
ROLE_BINDING=$(SRC_DIR)/role_binding.yaml
MODIFIED_SERVICE_ACCOUNT=$(DST_DIR)/service_account.yaml
MODIFIED_ROLE_BINDING=$(DST_DIR)/role_binding.yaml
RBAC_KUSTOMIZATION=$(SRC_DIR)/kustomization.yaml
MODIFIED_RBAC_KUSTOMIZATION=$(DST_DIR)/kustomization.yaml
DEFAULT_NS ?= eloq-elika-proxy-system
DEV_NS=""

.PHONY: copy-rbac
copy-rbac: kustomize
	@if [ -z "$(DEV_NS)" ]; then \
		echo "DEV_NS is not set."; \
		exit 1; \
	fi
	@rm -rf $(DST_DIR) && mkdir -p $(DST_DIR)
	@cp $(NAMESPACE) $(MODIFIED_NAMESPACE)
	@cp $(RBAC_KUSTOMIZATION) $(MODIFIED_RBAC_KUSTOMIZATION)
	@cp $(SERVICE_ACCOUNT) $(MODIFIED_SERVICE_ACCOUNT)
	@cp $(ROLE_BINDING) $(MODIFIED_ROLE_BINDING)
	@$(SED_IN_PLACE) 's/name: $(DEFAULT_NS)/name: $(DEV_NS)/' $(MODIFIED_NAMESPACE)
	@$(SED_IN_PLACE) 's/namespace: $(DEFAULT_NS)/namespace: $(DEV_NS)/' $(MODIFIED_SERVICE_ACCOUNT)
	@$(SED_IN_PLACE) 's/namespace: $(DEFAULT_NS)/namespace: $(DEV_NS)/' $(MODIFIED_ROLE_BINDING)
	@$(SED_IN_PLACE) 's/name: manager-rolebinding/name: $(DEV_NS)-manager-rolebinding/' $(MODIFIED_ROLE_BINDING)
	@echo "resources:\n - namespace.yaml \n - service_account.yaml\n - role_binding.yaml" | sed 's/\\n/\n/g' > $(DST_DIR)/kustomization.yaml
	$(KUSTOMIZE) build $(DST_DIR) | kubectl apply -f -

.PHONY: local-install
local-install: kustomize ## Install resources into the K8s be_cluster specified in ~/.kube/config.
	@if [ -z "$(DEV_NS)" ]; then \
		$(KUSTOMIZE) build config/overlays/dev | kubectl apply -f - ; \
	else \
		rm -rf tmp/config && \
		mkdir -p tmp/config/base tmp/config/overlays/dev && \
		cp -R config/base tmp/config/ && \
		cp -R config/overlays/dev tmp/config/overlays/ && \
		find tmp/config -type f -exec $(SED_IN_PLACE) 's/eloq-elika-proxy-system/$(DEV_NS)/g' {} + ; \
		find tmp/config -type f -exec $(SED_IN_PLACE) 's/name: elika-proxy-mutating/name: $(DEV_NS)-elika-proxy-mutating/g' {} + ; \
		if [ -n "$(PROXY_NS)" ]; then \
        	$(SED_IN_PLACE) 's/"true"/"$(PROXY_NS)"/g' tmp/config/base/webhook/manifests.yaml; \
        fi; \
		if [ -n "$(IMG)" ]; then \
			$(SED_IN_PLACE) 's/elika-proxy:latest/elika-proxy:$(IMG)/g' tmp/config/overlays/dev/statefulset_patch.yaml; \
		fi && \
		$(KUSTOMIZE) build tmp/config/overlays/dev | kubectl apply -f - ; \
	fi


.PHONY: deploy
deploy: rbac local-install

.PHONY: uninstall-local
uninstall-local: kustomize  ## Uninstall resources from the K8s be_cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@if [ -z "$(USE_DEV_NS)" ]; then \
		$(KUSTOMIZE) build config/rbac | kubectl delete --ignore-not-found=$(ignore-not-found) -f - && \
		$(KUSTOMIZE) build config/overlays/dev | kubectl delete --ignore-not-found=$(ignore-not-found) -f - ; \
	else \
		echo "Uninstalling resources from local tmp" && \
		$(KUSTOMIZE) build tmp/rbac | kubectl delete --ignore-not-found=$(ignore-not-found) -f - && \
		$(KUSTOMIZE) build tmp/config/overlays/dev | kubectl delete --ignore-not-found=$(ignore-not-found) -f - ; \
	fi

## Tool Versions
KUSTOMIZE_VERSION ?= v4.5.7

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be_cluster removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest