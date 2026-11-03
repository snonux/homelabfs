package controlledvalidation

import "reflect"

// UsesFacts identifies this provider's actual live verifier for controlled
// runtime assembly. Copied enrollment bytes cannot substitute another verifier.
func (p *Provider) UsesFacts(facts Facts) bool {
	return reflect.ValueOf(p.config.Facts) == reflect.ValueOf(facts)
}
