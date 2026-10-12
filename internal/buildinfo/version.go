package buildinfo

import "runtime"

const Release = "1.12.1"
const Version = Release + "-" + runtime.GOOS
