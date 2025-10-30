#!/bin/bash

# Fix Event_Queue
cat > nodes/Event_Queue/go.mod << 'EOF'
module github.com/ayden-boyko/Piranid/nodes/Event_Queue

go 1.21

require (
    Piranid/pkg v0.0.0
    Piranid/node v0.0.0
)

replace Piranid/pkg => ../../pkg
replace Piranid/node => ../../pkg/node
EOF

# Fix Auth
cat > nodes/Auth/go.mod << 'EOF'
module github.com/ayden-boyko/Piranid/nodes/Auth

go 1.21

require (
    Piranid/pkg v0.0.0
)

replace Piranid/pkg => ../../pkg
EOF

# Fix Logging
cat > nodes/Logging/go.mod << 'EOF'
module github.com/ayden-boyko/Piranid/nodes/Logging

go 1.21

require (
    Piranid/pkg v0.0.0
)

replace Piranid/pkg => ../../pkg
EOF

# Fix Notifications
cat > nodes/Notifications/go.mod << 'EOF'
module github.com/ayden-boyko/Piranid/nodes/Notifications

go 1.21

require (
    Piranid/pkg v0.0.0
)

replace Piranid/pkg => ../../pkg
EOF

# Now tidy everything
echo "Running go mod tidy on all modules..."
cd nodes/Event_Queue && go mod tidy && cd ../..
cd nodes/Auth && go mod tidy && cd ../..
cd nodes/Logging && go mod tidy && cd ../..
cd nodes/Notifications && go mod tidy && cd ../..

echo "Syncing workspace..."
go work sync

echo "Done! Restart your Go language server in VS Code."