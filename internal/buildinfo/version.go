package buildinfo

import "runtime"

const Release = "1.13.2"
const Version = Release + "-" + runtime.GOOS
