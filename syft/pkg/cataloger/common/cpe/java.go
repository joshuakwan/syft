package cpe

import (
	"strings"

	"github.com/anchore/syft/internal"
	"github.com/anchore/syft/syft/pkg"
	"github.com/scylladb/go-set/strset"
)

var (
	forbiddenProductGroupIDFields = strset.New("plugin", "plugins", "client")
	forbiddenVendorGroupIDFields  = strset.New("plugin", "plugins")

	domains = []string{
		"com",
		"org",
		"net",
		"io",
	}

	javaManifestGroupIDFields = []string{
		"Extension-Name",
		"Automatic-Module-Name",
		"Specification-Vendor",
		"Implementation-Vendor",
		"Bundle-SymbolicName",
		"Implementation-Vendor-Id",
		"Package",
		"Implementation-Title",
		"Main-Class",
		"Bundle-Activator",
	}
	javaManifestNameFields = []string{
		"Specification-Vendor",
		"Implementation-Vendor",
	}
)

func candidateProductsForJava(p pkg.Package) []string {
	return productsFromArtifactAndGroupIDs(artifactIDFromJavaPackage(p), groupIDsFromJavaPackage(p))
}

func candidateVendorsForJava(p pkg.Package) *fieldCandidateSet {
	gidVendors := vendorsFromGroupIDs(groupIDsFromJavaPackage(p))
	nameVendors := vendorsFromJavaManifestNames(p)
	return newCPRFieldCandidateFromSets(gidVendors, nameVendors)
}

func vendorsFromJavaManifestNames(p pkg.Package) *fieldCandidateSet {
	vendors := newCPRFieldCandidateSet()

	metadata, ok := p.Metadata.(pkg.JavaMetadata)
	if !ok {
		return vendors
	}

	if metadata.Manifest == nil {
		return vendors
	}

	for _, name := range javaManifestNameFields {
		if value, exists := metadata.Manifest.Main[name]; exists {
			if !startsWithDomain(value) {
				vendors.add(fieldCandidate{
					value:                 normalizeName(value),
					disallowSubSelections: true,
				})
			}
		}
		for _, section := range metadata.Manifest.NamedSections {
			if value, exists := section[name]; exists {
				if !startsWithDomain(value) {
					vendors.add(fieldCandidate{
						value:                 normalizeName(value),
						disallowSubSelections: true,
					})
				}
			}
		}
	}

	return vendors
}

func vendorsFromGroupIDs(groupIDs []string) *fieldCandidateSet {
	vendors := newCPRFieldCandidateSet()
	for _, groupID := range groupIDs {
		for i, field := range strings.Split(groupID, ".") {
			field = strings.TrimSpace(field)

			if len(field) == 0 {
				continue
			}

			if forbiddenVendorGroupIDFields.Has(strings.ToLower(field)) {
				continue
			}

			if i == 0 {
				continue
			}

			// e.g. jenkins-ci -> [jenkins-ci, jenkins]
			for _, value := range generateSubSelections(field) {
				vendors.add(fieldCandidate{
					value:                 value,
					disallowSubSelections: true,
				})
			}
		}
	}

	return vendors
}

func productsFromArtifactAndGroupIDs(artifactID string, groupIDs []string) []string {
	products := strset.New()
	if artifactID != "" {
		products.Add(artifactID)
	}

	for _, groupID := range groupIDs {
		isPlugin := strings.Contains(artifactID, "plugin") || strings.Contains(groupID, "plugin")

		for i, field := range strings.Split(groupID, ".") {
			field = strings.TrimSpace(field)

			if len(field) == 0 {
				continue
			}

			// don't add this field as a name if the name is implying the package is a plugin or client
			if forbiddenProductGroupIDFields.Has(strings.ToLower(field)) {
				continue
			}

			if i <= 1 {
				continue
			}

			// umbrella projects tend to have sub components that either start or end with the project name. We expect
			// to identify fields that may represent the umbrella project, and not fields that indicate auxiliary
			// information about the package.
			couldBeProjectName := strings.HasPrefix(artifactID, field) || strings.HasSuffix(artifactID, field)
			if artifactID == "" || (couldBeProjectName && !isPlugin) {
				products.Add(field)
			}
		}
	}

	return products.List()
}

func artifactIDFromJavaPackage(p pkg.Package) string {
	metadata, ok := p.Metadata.(pkg.JavaMetadata)
	if !ok {
		return ""
	}

	if metadata.PomProperties == nil {
		return ""
	}

	artifactID := strings.TrimSpace(metadata.PomProperties.ArtifactID)
	if startsWithDomain(artifactID) && len(strings.Split(artifactID, ".")) > 1 {
		// there is a strong indication that the artifact ID is really a group ID, don't use it
		return ""
	}
	return artifactID
}

func groupIDsFromJavaPackage(p pkg.Package) (groupIDs []string) {
	metadata, ok := p.Metadata.(pkg.JavaMetadata)
	if !ok {
		return nil
	}

	groupIDs = append(groupIDs, groupIDsFromPomProperties(metadata.PomProperties)...)
	groupIDs = append(groupIDs, groupIDsFromJavaManifest(metadata.Manifest)...)

	return groupIDs
}

func groupIDsFromPomProperties(properties *pkg.PomProperties) (groupIDs []string) {
	if properties == nil {
		return nil
	}

	if startsWithDomain(properties.GroupID) {
		groupIDs = append(groupIDs, strings.TrimSpace(properties.GroupID))
	}

	// sometimes the publisher puts the group ID in the artifact ID field unintentionally
	if startsWithDomain(properties.ArtifactID) && len(strings.Split(properties.ArtifactID, ".")) > 1 {
		// there is a strong indication that the artifact ID is really a group ID
		groupIDs = append(groupIDs, strings.TrimSpace(properties.ArtifactID))
	}

	return groupIDs
}

func groupIDsFromJavaManifest(manifest *pkg.JavaManifest) (groupIDs []string) {
	if manifest == nil {
		return nil
	}
	// attempt to get group-id-like info from the MANIFEST.MF "Automatic-Module-Name" and "Extension-Name" field.
	// for more info see pkg:maven/commons-io/commons-io@2.8.0 within cloudbees/cloudbees-core-mm:2.263.4.2
	// at /usr/share/jenkins/jenkins.war:WEB-INF/plugins/analysis-model-api.hpi:WEB-INF/lib/commons-io-2.8.0.jar
	// as well as the ant package from cloudbees/cloudbees-core-mm:2.277.2.4-ra.
	for _, name := range javaManifestGroupIDFields {
		if value, exists := manifest.Main[name]; exists {
			if startsWithDomain(value) {
				groupIDs = append(groupIDs, value)
			}
		}
		for _, section := range manifest.NamedSections {
			if value, exists := section[name]; exists {
				if startsWithDomain(value) {
					groupIDs = append(groupIDs, value)
				}
			}
		}
	}

	return groupIDs
}

func startsWithDomain(value string) bool {
	return internal.HasAnyOfPrefixes(value, domains...)
}
