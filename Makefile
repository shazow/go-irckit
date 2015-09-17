all: $(BINARY)

$(BINARY): $(SOURCES)
	go build -ldflags "-X main.version=`git describe --long --tags --dirty --always`"

deps:
	go get ./...

build: $(BINARY)

clean:
	rm $(BINARY)

test:
	go test ./...
	golint ./...
	go build examples/irckit-server/ -o /dev/null
