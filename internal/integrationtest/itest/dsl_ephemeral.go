package itest

// noRoots blanks both root variables of the helper base environment, which
// is how a scenario reaches the unconfigured serve default. An empty
// environment value is unset (architecture/config.md).
var NoRoots = []string{
	"SLIVINGDOC_WORKSPACE_ROOT=",
	"SLIVINGDOC_PRIVATE_ROOT=",
}
