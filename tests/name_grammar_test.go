package tests

import (
	"encoding/json"
	"fmt"
	"strings"

	"aminte/text"
	"golang.org/x/text/unicode/norm"
)

// nameGrammar executes the contract's addressability vectors under docs overlays,
// separately from the production create/save/rename regression tests.
func nameGrammar(s *S) {
	page := strings.ReplaceAll(s.d.Page("contract/titles-and-wikilinks.md"), "\r\n", "\n")
	_, rest, ok := strings.Cut(page, "<!-- reference-name-vectors -->")
	if !ok {
		stop("missing reference-name vectors")
	}
	_, rest, ok = strings.Cut(rest, "```json\n")
	if !ok {
		stop("missing vector JSON")
	}
	encoded, _, ok := strings.Cut(rest, "```")
	if !ok {
		stop("unterminated vector JSON")
	}
	var vectors []struct {
		Name, Key string
		Accepted  bool
	}
	if err := json.Unmarshal([]byte(encoded), &vectors); err != nil {
		stop("parse reference-name vectors: %v", err)
	}
	s.K("reference-name grammar vectors are complete", len(vectors) == 30)
	for _, v := range vectors {
		s.K("reference-name predicate agrees with "+v.Name, text.ValidTitle(v.Name) == v.Accepted)
		if !v.Accepted {
			continue
		}
		matches := text.TitleKey(v.Name) == v.Key
		for _, spelling := range []string{v.Name, norm.NFC.String(v.Name)} {
			for _, body := range []string{"See [[" + spelling + "]].", "See ![[" + spelling + "]].", "See [[" + spelling + "|display]]."} {
				got := text.Candidates(body)
				matches = matches && len(got) == 1
				if len(got) == 1 {
					matches = matches && text.TitleKey(got[0]) == v.Key
				}
			}
		}
		s.K("raw and NFC reference forms preserve key for "+v.Name, matches)
	}
	c := s.fresh()
	for i, spelling := range []string{"Left [ bracket", "Right ] bracket"} {
		_, err := c.tryIdentity("page", spelling, nil)
		s.K(fmt.Sprintf("registry rejects bracket spelling %d", i), err != nil && strings.Contains(err.Error(), "entity_names_title_safe"), err)
	}
}
