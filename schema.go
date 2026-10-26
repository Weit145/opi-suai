package schema

import _ "embed"

//go:embed migrations/001_initial.sql
var Initial string
