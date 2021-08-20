package cpe

import "github.com/anchore/syft/syft/pkg"

func candidateVendorsForRPM(p pkg.Package) *fieldCandidateSet {
	metadata, ok := p.Metadata.(pkg.RpmdbMetadata)
	if !ok {
		return nil
	}

	vendors := newCPRFieldCandidateSet()

	if metadata.Vendor != "" {
		vendors.add(fieldCandidate{
			value:                 normalizeTitle(metadata.Vendor),
			disallowSubSelections: true,
		})
	}

	return vendors
}
