package assignimage

import (
	"fmt"
	"regexp"
	"strings"
)

type image struct {
	domain string
	path   string
	tag    string
}

var (
	// domainRegexp defines a schema for a domain component. Primarily, this is to
	// avoid the possibility of injecting `/` tokens, which could cause part of
	// the domain component to "leak" over to the location component, ultimately
	// causing non convergence.
	domainRegexp = regexp.MustCompile(`^\w[\w.\-_]*(:\d+)?$`)

	// pathRegexp defines a schema for a location component. It follows the convention
	// specified in the docker distribution reference. The regex  restricts
	// location-components to start with an alphanumeric character, with following
	// parts able to be separated by a separator (one period, one or two
	// underscore and multiple dashes).
	pathRegexp = regexp.MustCompile(`^[a-z0-9]+(?:(?:(?:[._/]|__|[-]*)[a-z0-9]+)+)?`)

	// tagRegexp defines a schema for a tag component. It must start with `:` or `@`.
	tagRegexp = regexp.MustCompile(`(^:[\w][\w.-]{0,127}$)|(^@[A-Za-z][A-Za-z0-9]*([-_+.][A-Za-z][A-Za-z0-9]*)*[:][0-9A-Fa-f]{32,}$)`)
)

func mutateImage(domain, path, tag, mutableImgRef string) string {
	oldImg := newImage(mutableImgRef)
	newImg := oldImg.newMutatedImage(domain, path, tag)
	return newImg.fullRef()
}

func newImage(imageRef string) image {
	domain, remainder := splitDomain(imageRef)
	var pt string
	tag := ""
	if tagSep := strings.IndexAny(remainder, ":@"); tagSep > -1 {
		pt = remainder[:tagSep]
		tag = remainder[tagSep:]
	} else {
		pt = remainder
	}

	return image{domain: domain, path: pt, tag: tag}
}

func (img image) newMutatedImage(domain, path, tag string) image {
	return image{
		domain: ignoreUnset(img.domain, domain),
		path:   ignoreUnset(img.path, path),
		tag:    ignoreUnset(img.tag, tag),
	}
}

// ignoreUnset returns `new` if `new` is set, otherwise it returns `old`.
func ignoreUnset(old, new string) string {
	if new != "" {
		return new
	}
	return old
}

func (img image) fullRef() string {
	domain := img.domain
	if domain != "" {
		domain += "/"
	}
	return domain + img.path + img.tag
}

func splitDomain(name string) (domain, remainder string) {
	i := strings.IndexRune(name, '/')
	if i == -1 || (!strings.ContainsAny(name[:i], ".:") && name[:i] != "localhost") {
		return "", name
	}
	return name[:i], name[i+1:]
}

func validateImageParts(domain, path, tag string) error {
	if domain == "" && path == "" && tag == "" {
		return fmt.Errorf("at least one of [assignDomain, assignPath, assignTag] must be set")
	}
	if domain != "" && !domainRegexp.MatchString(domain) {
		return fmt.Errorf("assignDomain %q does not match pattern %q", domain, domainRegexp.String())
	}
	// match the whole string for path (anchoring with `$` is tricky here)
	if path != "" && path != pathRegexp.FindString(path) {
		return fmt.Errorf("assignPath %q does not match pattern %q", path, pathRegexp.String())
	}
	if tag != "" && !tagRegexp.MatchString(tag) {
		return fmt.Errorf("assignTag %q does not match pattern %q", tag, tagRegexp.String())
	}

	// Check if the path looks like a domain string, and the domain is not set.
	// This prevents part of the path field from "leaking" to the domain, causing
	// nonconvergent behavior.
	// For example, suppose: domain="", path="gcr.io/repo", tag=""
	// Suppose no value is currently set on the mutable, so the result is
	// just "gcr.io/repo". When this value mutated again, "gcr.io" is parsed into
	// the domain component, so the result would be "gcr.io/gcr.io/repo".
	if domain == "" {
		if d, _ := splitDomain(path); d != "" {
			return fmt.Errorf("assignDomain must be set if assignPath %q can be interpretted as part of a domain", path)
		}
	}

	return nil
}
