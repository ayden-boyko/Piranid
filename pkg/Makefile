# Defaults
PROTO_DIR ?= proto
GEN_DIR ?= gen_protos
PROTO_FILE ?= notifications.proto

# Construct full path
PROTO_SRC := $(PROTO_DIR)/$(PROTO_FILE)

.PHONY: all proto clean

all: proto

proto:
	@mkdir -p $(GEN_DIR)
	@echo "Generating $(PROTO_FILE) from $(PROTO_DIR) to $(GEN_DIR)..."
	protoc --go_out=$(GEN_DIR) \
	       --go_opt=module=github.com/ayden-boyko/Piranid \
	       --go-grpc_out=$(GEN_DIR) \
	       --go-grpc_opt=module=github.com/ayden-boyko/Piranid \
	       -I=$(PROTO_DIR) \
	       $(PROTO_SRC)

clean:
	@echo "Cleaning $(GEN_DIR)..."
	rm -rf $(GEN_DIR)

rebuild: clean proto

# # Use all defaults
# make proto

# # Just change the proto file
# make proto PROTO_FILE=logging.proto

# # Change output directory
# make proto GEN_DIR=../../pkg/proto/notifications

# # Specify everything
# make proto PROTO_DIR=../shared PROTO_FILE=events.proto GEN_DIR=./gen

# # Generate to shared pkg location
# make proto \
  PROTO_DIR=proto \
  PROTO_FILE=notifications/v1/notifications.proto \
  GEN_DIR=../../

