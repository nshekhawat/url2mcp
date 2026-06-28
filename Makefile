.PHONY: build build-client start stop test clean run

BINARY   = url2mcp
CLIENT   = mcp-client
PIDFILE  = /tmp/$(BINARY).pid

build: $(BINARY) $(CLIENT)

$(BINARY):
	go build -o $@ .

$(CLIENT):
	go build -o $@ ./cmd/$(CLIENT)

start: $(BINARY)
	@echo "Starting $(BINARY)..."
	./$(BINARY) &
	@echo $$! > $(PIDFILE)
	@echo "PID: $$(cat $(PIDFILE))"

stop:
	@echo "Stopping $(BINARY)..."
	-pkill -f "$(BINARY)" 2>/dev/null || true
	-rm -f $(PIDFILE)
	@echo "Stopped."

run: $(BINARY)
	./$(BINARY)

test:
	go test -race -v ./...

clean:
	rm -f $(BINARY) $(CLIENT)