package core

import (
	"regexp"
	"strings"

	"aminte/siren"
)

// writerRE is the grammar of a writer name: the CHECKs entities_source, measurements_source and links_source.
var writerRE = regexp.MustCompile(`^[a-z0-9_:.-]{1,64}$`)

// WriterName says that name is a writer name. A surface that reads a writer name and meets no CHECK of it (an
// owner-only action) asks it here.
func WriterName(name string) bool { return writerRE.MatchString(name) }

// AgentWriter says that a writer is an agent (agent:*) or an import (import:*): the writers that the owner's rules bind.
func AgentWriter(source string) bool {
	return strings.HasPrefix(source, siren.AgentPrefix) || strings.HasPrefix(source, siren.ImportPrefix)
}
