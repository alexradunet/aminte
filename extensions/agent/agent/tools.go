package agent

import (
	"lifelog/client/tools"
	"lifelog/siren"
)

// toolsOf is the tool list of the catalog for this agent. tools.Of leaves out every owner action, and so say: a model
// that could call it would write owner turns that the agent answers, a loop. This function leaves out two more:
//   - reply: the agent writes its turn with it, once for each run of the model. A model that could call it would write
//     a turn of its own, and the page would have two replies to one turn.
//   - An action with a file field (a recording, a picture): its value would be the path of any file the process can
//     read, and the model acts on text that any surface wrote to the page. A chat has no files for now
//     (docs/plans/145-whatsapp.md, "text only").
func toolsOf(list []siren.Action) ([]tools.Tool, error) {
	return tools.Of(list, func(a siren.Action) bool { return a.Name != "reply" && !takesFile(a) })
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
