package central

import (
	"encoding/json"
	"fmt"
	"os"
)

// Invalid state must never look like an empty installation and create a second
// set of topics. Leave the file intact for recovery and let supervision retry.
func readForumState(path string) (forumState, error) {
	st := forumState{Nodes: map[string]forumNodeState{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err = json.Unmarshal(b, &st); err != nil {
		return st, fmt.Errorf("invalid forum state; refusing to create replacement topics: %w", err)
	}
	if st.Nodes == nil {
		st.Nodes = map[string]forumNodeState{}
	}
	return st, nil
}
