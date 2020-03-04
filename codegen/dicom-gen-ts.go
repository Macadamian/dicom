package main

import (
	"encoding/json"
	"fmt"
	"github.com/gradienthealth/dicom/dicomtag"
	"github.com/macadamian/dicom/dicom2019bdata"
	"os"
	"strconv"
	"strings"
	"unicode"
)

func parseTag(tag string) (dicomtag.Tag, error) {
	parts := strings.Split(strings.Trim(tag, "()"), ",")
	group, err := strconv.ParseInt(parts[0], 16, 0)
	if err != nil {
		return dicomtag.Tag{}, err
	}
	elem, err := strconv.ParseInt(parts[1], 16, 0)
	if err != nil {
		return dicomtag.Tag{}, err
	}
	return dicomtag.Tag{Group: uint16(group), Element: uint16(elem)}, nil
}

func fixKeyword(kw string) string {
	if len(kw) == 0 {
		return kw
	}

	r := []rune(kw)
	if unicode.IsLower(r[0]) {
		r[0] = unicode.ToUpper(r[0])
	}

	return string(r)
}

////  Schema (with some additions)

type SchemaDef struct {
	ClassDefs  []ClassDef
	TagDefs    map[string]TagDef
	ModuleDefs map[string]ModuleDef
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
	Audit []TagAudit
}

type TagAudit struct {
	Name string
	Type string
	Path string
}

type TagDef struct {
	Keyword    string
	VR         []string
	VM         string
	Deidentify string
}

func main() {
	sch := SchemaDef{}

	err := json.Unmarshal([]byte(dicom2019bdata.SchemaStr), &sch)
	if err != nil {
		panic(err)
	}

	out := os.Stdout

	// TODO this should be declared in a separate file
	fmt.Fprintf(out, `import "reflect-metadata";
export const tagMeta = Symbol("tag");
export const vrMeta = Symbol("vr");
export const vmMeta = Symbol("vm");
export const typesMeta = Symbol("types");
export const deidentifyMeta = Symbol("deidentify");
export const sopClassMeta = Symbol("SOPClassUID");

function tag(tagString: string) {
    return Reflect.metadata(tagMeta, tagString);
}
function vr(vrString: string) {
    return Reflect.metadata(vrMeta, vrString);
}
function vm(vmString: string) {
    return Reflect.metadata(vmMeta, vmString);
}
function types(typesString: string) {
    return Reflect.metadata(typesMeta, typesString);
}
function deidentify(deidentifyString: string) {
    return Reflect.metadata(deidentifyMeta, deidentifyString);
}
function SOPClassUID(sopClassUIDString: string) {
	return Reflect.metadata(sopClassMeta, sopClassUIDString);
}
`)
	for _, cd := range sch.ClassDefs {
		name := cd.Name
		name = strings.Replace(name, " ", "", -1)
		name = strings.Replace(name, "-", "", -1)
		name = strings.Replace(name, "/", "", -1)
		if !unicode.IsLetter([]rune(name)[0]) {
			name = "A" + name
		}

		fmt.Fprintf(out, "@SOPClassUID(\"%s\")\n", cd.SOPClassUid)
		fmt.Fprintf(out, "export class %s {\n", name)

		for _, mdu := range cd.Modules {
			name = mdu.Name
			name = strings.Replace(name, " ", "", -1)
			name = strings.Replace(name, "-", "", -1)
			name = strings.Replace(name, "/", "", -1)

			if !unicode.IsLetter([]rune(name)[0]) {
				name = "A" + name
			}

			typ := name

			// TODO what happened here?
			if name == "EnhancedXAXRFImage" {
				name = "//" + name
			}

			var optional = ""
			var initialValue = ""
			if mdu.Usage == "U" {
				optional = "?"
			} else {
				initialValue = fmt.Sprintf(" = new %s()", name);
			}

			fmt.Fprintf(out, "\t%s%s: %s%s;\n", name, optional, typ, initialValue, )
		}

		fmt.Fprintf(out, "}\n\n")
	}

	// First, do a scan to divide up the tags into their structs
	structdefs := map[string]*ModuleDef{}
	for name, md := range sch.ModuleDefs {
		name = strings.Replace(name, " ", "", -1)
		name = strings.Replace(name, "-", "", -1)
		name = strings.Replace(name, "/", "", -1)
		if !unicode.IsLetter([]rune(name)[0]) {
			name = "A" + name
		}

		sd := &ModuleDef{}
		structdefs[name] = sd

		for _, tgu := range md.Tags {
			for idx, tgs := range tgu.Path {
				if idx == len(tgu.Path)-1 {
					parentstruct := sd

					if idx > 0 {
						parentg := tgu.Path[idx-1]
						parentd := sch.TagDefs[parentg]
						parentstruct = structdefs[fixKeyword(parentd.Keyword)]
					}

					// TODO there's something wrong with the 2019b data where this path is found: Intraocular Lens Calculations {[(0022,1300) (0046,0110) (0046,0116) (0008,0100) (0008,0102)] 1C}
					if parentstruct == nil {
						fmt.Fprintf(os.Stderr, "Non sequence parent detected: %v %v\n", name, tgu)
						break
					}

					// Check if this tag is already there in the parent struct
					found := false
					for idx, t := range parentstruct.Tags {
						if t.Path[0] == tgs {
							found = true

							t.Audit = append(t.Audit, TagAudit{name, t.Type, strings.Join(tgu.Path[:len(tgu.Path)-1], ",")})

							// Disagreements on the type get downgraded to 3 (optional)
							if t.Type != tgu.Type {
								t.Type = "3"
							}

							parentstruct.Tags[idx] = t

							break
						}
					}

					if !found {
						newt := TagUsage{Path: []string{tgs}, Type: tgu.Type, Audit: []TagAudit{TagAudit{name, tgu.Type, strings.Join(tgu.Path[:len(tgu.Path)-1], ",")}}}
						parentstruct.Tags = append(parentstruct.Tags, newt)
					}
				}

				// Ensure that this sequence is registered as a struct
				td := sch.TagDefs[tgs]
				if td.VR[0] == "SQ" {
					_, ok := structdefs[fixKeyword(td.Keyword)]
					if !ok {
						structdefs[fixKeyword(td.Keyword)] = &ModuleDef{}
					}
				}
			}
		}
	}

	for name, md := range structdefs {
		name = strings.Replace(name, " ", "", -1)
		name = strings.Replace(name, "-", "", -1)
		name = strings.Replace(name, "/", "", -1)
		if !unicode.IsLetter([]rune(name)[0]) {
			name = "A" + name
		}

		fieldNames := map[string]bool{}

		allAudits := []TagAudit{}

		// Scan ahead to find all of the audits
		for _, tgu := range md.Tags {
			for _, a := range tgu.Audit {
				found := false
				for _, a2 := range allAudits {
					if a.Name == a2.Name && a.Path == a2.Path {
						found = true
						break
					}
				}

				if !found {
					allAudits = append(allAudits, a)
				}
			}
		}

		fmt.Fprintf(out, "export class %s {\n", name)
		for _, tgu := range md.Tags {
			if len(tgu.Path) != 1 {
				panic(fmt.Errorf("These should all be single segment paths at this point"))
			}

			// If there are non-existent paths to this tag usage then we downgrade to type 3
			//  so that they can be left out in those paths
			if len(allAudits) != len(tgu.Audit) {
				tgu.Type = "3"
			} else {
				for _, a1 := range allAudits {
					found := false
					for _, a2 := range tgu.Audit {
						if a1.Name == a2.Name && a1.Path == a2.Path {
							found = true
							break
						}
					}

					if !found {
						tgu.Type = "3"
					}
				}

				for _, a1 := range tgu.Audit {
					found := false
					for _, a2 := range allAudits {
						if a1.Name == a2.Name && a1.Path == a2.Path {
							found = true
							break
						}
					}

					if !found {
						tgu.Type = "3"
					}
				}
			}

			td := sch.TagDefs[tgu.Path[0]]

			name = fixKeyword(td.Keyword)
			counter := 0
			for {
				if _, exists := fieldNames[name]; !exists {
					break
				}
				counter++
				name = fmt.Sprintf("%s%d", fixKeyword(td.Keyword), counter)
			}
			fieldNames[name] = true

			dcmtag, err := parseTag(tgu.Path[0])
			if err != nil {
				panic(err)
			}

			vrk := dicomtag.GetVRKind(dcmtag, td.VR[0])
			typ := "string"

			switch vrk {
			case dicomtag.VRStringList:
				typ = "string"
			case dicomtag.VRBytes:
				typ = "string"
			case dicomtag.VRUInt16List:
				typ = "number"
			case dicomtag.VRUInt32List:
				typ = "number"
			case dicomtag.VRInt16List:
				typ = "number"
			case dicomtag.VRInt32List:
				typ = "number"
			case dicomtag.VRFloat32List:
				typ = "number"
			case dicomtag.VRFloat64List:
				typ = "number"
			case dicomtag.VRSequence:
				typ = name
			case dicomtag.VRItem:
				typ = "any" // TODO what should be the mapping here?
			case dicomtag.VRTagList:
				typ = "string"
			case dicomtag.VRDate:
				typ = "string"
			case dicomtag.VRPixelData:
				typ = "string"
			}

			// Any range or sequence just translated into an array
			if strings.Contains(td.VM, "-") || td.VR[0] == "SQ" {
				typ = "Array<" + typ + ">"
			} else if _, err := strconv.Atoi(td.VM); err == nil && td.VM != "1" {
				typ = fmt.Sprintf("Array<%s>", typ)
			}

			initialValue := ""

			// Optional types that aren't already arrays are made into optional
			//  so that they can be null. Slices are exempt since their zero value is
			//  already a kind of pointer that can be nil.
			if tgu.Type != "1" && tgu.Type != "2" && !strings.Contains(typ, "Array<") {
				name = name + "?"
			} else {
				// Figure out what initial values would be for non-optionals
				if typ == "string" {
					initialValue = " = \"\""
				} else if typ == "number" {
					initialValue = " = 0"
				} else if strings.HasPrefix(typ, "Array<") {
					initialValue = " = []"
				} else {
					initialValue = " = new " + typ + "()"
				}
			}

			audits := "["
			for _, a := range tgu.Audit {
				if audits != "[" {
					audits = audits + ","
				}

				path := a.Name
				if a.Path != "" {
					path = path + "," + a.Path
				}

				audits = audits + "{" + path + "," + a.Type + "}"
			}
			audits = audits + "]"

			fmt.Fprintf(out, "\t@tag(\"%s\")\n", dcmtag)
			fmt.Fprintf(out, "\t@vr(\"%s\")\n", td.VR[0])
			fmt.Fprintf(out, "\t@vm(\"%s\")\n", td.VM)
			fmt.Fprintf(out, "\t@deidentify(\"%s\")\n", td.Deidentify)
			fmt.Fprintf(out, "\t@types(\"%s\")\n", audits)
			fmt.Fprintf(out, "\t%s: %s%s;\n\n", name, typ, initialValue)
		}
		fmt.Fprintf(out, "}\n\n")
	}
}
