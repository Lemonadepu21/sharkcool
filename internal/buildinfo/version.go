package buildinfo

import "runtime"

const Release = "1.13.0"
const Version = Release + "-" + runtime.GOOS
