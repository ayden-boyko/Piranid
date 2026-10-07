package utils

import "time"

// timeLayout is the storage format for date_created columns. The schema stores
// dates as VARCHAR, so the layout must be fixed in one place to keep writes and
// any future reads consistent.
const timeLayout = time.RFC3339Nano
