SHELL := /bin/bash
TODO_STACK := skills/todo-stack/scripts/todo-stack

.PHONY: all test vet errcheck compile

all: test vet errcheck compile

test:
	GOWORK=off go -C $(TODO_STACK) test ./...

vet:
	GOWORK=off go -C $(TODO_STACK) vet ./...

errcheck:
	cd $(TODO_STACK) && GOWORK=off errcheck ./...

compile:
	GOWORK=off CGO_ENABLED=0 go -C $(TODO_STACK) build -buildvcs=false -trimpath -o /dev/null .
