# Makefile

# Define the output directory and binary name
BINDIR := bin
BINARY := coadmin-cli

# Default target
all: build

# Create the bin directory if it doesn't exist and build the binary
build:
	mkdir -p $(BINDIR)
	go build -o $(BINDIR)/$(BINARY) ./main.go

# Clean up the bin directory
clean:	
	rm -rf $(BINDIR)

# Phony targets
.PHONY: all build clean