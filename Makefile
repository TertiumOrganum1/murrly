SHELL := /bin/bash
BIN := bin/murrly
WHISPER_DIR := third_party/whisper.cpp
WHISPER_BUILD := $(WHISPER_DIR)/build
MODEL ?= large-v3
MODEL_DIR := models

UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
  include mk/linux.mk
else ifeq ($(UNAME_S),Darwin)
  include mk/darwin.mk
else
  $(error Unsupported OS: $(UNAME_S))
endif

.PHONY: all whisper model build test install start stop restart autostart uninstall-autostart clean icons

all: build

whisper: $(WHISPER_BUILD)/src/libwhisper.a

model: whisper
	mkdir -p $(MODEL_DIR)
	$(WHISPER_DIR)/models/download-ggml-model.sh $(MODEL) $(MODEL_DIR)

icons:
	scripts/build-icons.sh

clean:
	rm -rf bin build
	rm -rf $(WHISPER_BUILD)
