BINARY   := 3ax-ui
TARGET   := target
TMP      := tmp

.PHONY: all build clean tmp-dir target-dir

all: build

## build — compile binary into target/
build: target-dir
	go build -o $(TARGET)/$(BINARY) .

## run — build and run from target/
run: build
	./$(TARGET)/$(BINARY)

## clean — remove target/ and tmp/
clean:
	rm -rf $(TARGET) $(TMP)

## tmp-dir / target-dir — create dirs if missing
tmp-dir:
	@mkdir -p $(TMP)

target-dir:
	@mkdir -p $(TARGET)
