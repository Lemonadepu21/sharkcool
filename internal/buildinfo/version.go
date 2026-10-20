package buildinfo

import "runtime"

const Release = "1.14.0"
const Version = Release + "-" + runtime.GOOS
