package syntax

import gts "github.com/odvcencio/gotreesitter"

// SignatureParameters joins a parameter list to its innermost named callable.
func SignatureParameters(signatures []gts.SignatureFact, parameters *gts.Node, lang *gts.Language) ([]gts.ParameterFact, bool) {
	span := NodeSpan(parameters)
	nonempty := false
	for i := 0; i < parameters.NamedChildCount(); i++ {
		if parameters.NamedChild(i).Type(lang) != "comment" {
			nonempty = true
			break
		}
	}
	var found *gts.SignatureFact
	for i := range signatures {
		s := &signatures[i]
		// An outer empty signature must not erase an unbound callable's names.
		if len(s.Parameters) == 0 && nonempty {
			continue
		}
		contained := true
		for _, p := range s.Parameters {
			if int(p.StartByte) < span.Start || int(p.EndByte) > span.End {
				contained = false
				break
			}
		}
		if contained && int(s.StartByte) <= span.Start && int(s.EndByte) >= span.End &&
			(found == nil || s.EndByte-s.StartByte < found.EndByte-found.StartByte) {
			found = s
		}
	}
	if found == nil {
		return nil, false
	}
	return found.Parameters, true
}
