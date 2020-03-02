package dicom

// Unmarshal data from a sub-package's SchemaStr into a SchemaDef using the encoding/json package
type SchemaDef struct {
	// List of SOP Class definitions that form this version of the schema.
	ClassDefs  []ClassDef
	// Map of DICOM tag (e.g. "(0008,001a)") to DICOM tag definitions included in this version of the schema.
	TagDefs    map[string]TagDef
	// Map of module name to their definition in this version of the schema.
	ModuleDefs map[string]ModuleDef
}

// An SOP Class definition describes the name, SOPClassUID and modules that form a valid DICOM
// instance of this class.
type ClassDef struct {
	SOPClassUid string
	Name        string
	Modules     []ModuleUsage
}

// A module usage describes a module that is used as part of an SOP Class and the
// usage, whether required, option or conditionally optional.
type ModuleUsage struct {
	// Name provides a name to this module usage and also a key into the SchemaDef.ModuleDefs to look
	// up additional information. 
	Name  string
	// Usage is either "M" (mandatory), "U" (user optional), or "C" (conditionally optional)
	Usage string
}

// A module definition is a list of tag usages that forms this module.
type ModuleDef struct {
	Tags []TagUsage
}

// A tag usage is a usage of one or more tags in a path within the DICOM instance with a
// type that determines whether the tag is required, optional, may be present or not.
type TagUsage struct {
	// A path within a DICOM instance of tags for this usage (e.g. ["(0040,0555)","(0040,a040)"]) 
	Path []string
	// A type of this usage:
	//   "1" Required to be in the SOP Instance and shall have a valid value.
	//   "2" Required to be in the SOP Instance but may contain the value of "unknown", or a zero length value.
	//   "3" Optional. May or may not be included and could be zero length.
	//   "1C" Conditional. If a condition is met, then it is a Type 1 (required, cannot be zero). If condition is not met, then the tag is not sent.
	//   "2C" Conditional. If condition is met, then it is a Type 2 (required, zero length OK). If condition is not met, then the tag is not sent.
	Type string
}

// A tag defintion provide information about a DICOM tag within this version of the specification
// with the plain text keyword, the value representation, multiplicity and deidentification metadata.
type TagDef struct {
	// The keyword is a plain text keyword for this tag that is guaranteed to be unique
	Keyword    string
	// The VR (Value Representation) defines one or more types for this tag. The vast majority only have one type.
	// See http://dicom.nema.org/medical/dicom/current/output/html/part05.html#sect_7.5 for descriptions of the
	// different representations.
	// See https://godoc.org/github.com/gradienthealth/dicom#Element for how these are mapped to Go types.
	VR         []string
	// The VM (Value Multiplicity) defines the range of the number of values for this tag.
	VM         string
	// Deidentify provides metadata regarding the handling of this tag when deidentifying DICOM information.
	// If empty, the spec does not expect that this tag is likely to contain personal health information. Otherwise,
	// values are described in Section E.1.1 of PS 3.15 of the DICOM spec for detailed explanations.
	Deidentify string
}