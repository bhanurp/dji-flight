APP     := dji-flight
MODULE  := github.com/bhanureddy/dji-flight
VERSION := 0.2.0
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
OUTDIR  := dist

.PHONY: build
build:
	go build $(LDFLAGS) -o $(APP) ./cmd/$(APP)

.PHONY: release
release: $(OUTDIR)
	GOOS=darwin  GOARCH=amd64  go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-darwin-amd64     ./cmd/$(APP)
	GOOS=darwin  GOARCH=arm64  go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-darwin-arm64     ./cmd/$(APP)
	GOOS=linux   GOARCH=amd64  go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-linux-amd64      ./cmd/$(APP)
	GOOS=linux   GOARCH=arm64  go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-linux-arm64      ./cmd/$(APP)
	GOOS=windows GOARCH=amd64  go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-windows-amd64.exe ./cmd/$(APP)
	@echo "Built all targets in $(OUTDIR)/"

$(OUTDIR):
	mkdir -p $(OUTDIR)

.PHONY: clean
clean:
	rm -rf $(OUTDIR) $(APP)

.PHONY: test
test:
	go test ./...

.PHONY: install
install:
	go install $(LDFLAGS) ./cmd/$(APP)

.PHONY: install-local
install-local: build
	cp $(APP) /usr/local/bin/$(APP)
	@echo "Installed $(APP) to /usr/local/bin/"
