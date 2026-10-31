package agent

import (
	"lifelog/client/tools"
	"lifelog/siren"
)

// toolsOf is the tool list of the catalog for this agent. It leaves out two kinds of action:
//   - An action with a file field (a recording, a picture): its value would be the path of any file the process can
//     read, and the model acts on text that any surface wrote to the page. A chat has no files for now
//     (docs/plans/145-whatsapp.md, "text only").
//
// say is the owner's action, so tools.Of leaves it out: a model that could call it would write owner turns that the
// agent answers, a loop.
func toolsOf(list []siren.Action) ([]tools.Tool, error) {
	return tools.Of(list, func(a siren.Action) bool { return !takesFile(a) })
}

// takesFile says whether an action has a file field.
func takesFile(a siren.Action) bool {
	for _, f := range a.Fields {
		if f.Type == "file" {
			return true
		}
	}
	return false
}
