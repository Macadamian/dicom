package dicom

////  Schema Data

type SchemaDef struct {
	ClassDefs  []*ClassDef
	TagDefs    map[string]*TagDef
	ModuleDefs map[string]*ModuleDef
}

type ClassDef struct {
	SOPClassUid string
	Name        string
	Modules     []ModuleUsage
}

type ModuleUsage struct {
	Name  string
	Usage string
}

type ModuleDef struct {
	Tags []TagUsage
}

type TagUsage struct {
	Path []string
	Type string
}

type TagDef struct {
	Keyword    string
	VR         []string
	VM         string
	Deidentify string
}