# Go parameters
GOCMD=GO111MODULE=on go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test

all: test build
build:
	rm -rf target/
	mkdir target/
	cp cmd/comet/comet-example.toml target/comet.toml
	cp cmd/logic/logic-example.toml target/logic.toml
	cp cmd/job/job-example.toml target/job.toml
	$(GOBUILD) -o target/comet cmd/comet/main.go
	$(GOBUILD) -o target/logic cmd/logic/main.go
	$(GOBUILD) -o target/job cmd/job/main.go

test:
	$(GOTEST) -v ./...

clean:
	rm -rf target/

run:
	nohup target/logic -logic.conf=target/logic.toml -logic.region=sh -logic.zone=sh001 -logic.deploy.env=dev -logic.weight=10 >/dev/null 2>&1 > target/logic.log &
	nohup target/comet -comet.conf=target/comet.toml -comet.region=sh -comet.zone=sh001 -comet.deploy.env=dev -comet.weight=10 -comet.addrs=127.0.0.1 -comet.debug=true >/dev/null 2>&1 > target/comet.log &
	nohup target/job -job.conf=target/job.toml -job.region=sh -job.zone=sh001 -job.deploy.env=dev >/dev/null 2>&1 > target/job.log &

stop:
	pkill -f target/logic
	pkill -f target/job
	pkill -f target/comet
