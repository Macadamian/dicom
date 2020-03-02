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

	out, err := os.Create("../dicom2019b/dicom-2019b-pkg.go")
	if err != nil {
		panic(err)
	}
	defer out.Close()

	fmt.Fprintf(out, "package dicom2019b\n\n")

	fmt.Fprintf(out, "import \"github.com/gradienthealth/dicom\"\n")
	fmt.Fprintf(out, "import \"github.com/gradienthealth/dicom/dicomtag\"\n\n")

	fmt.Fprintf(out, "// TODO embedded spec references as godocs\n")
	fmt.Fprintf(out, "// TODO multiplicities for sequences\n")
	fmt.Fprintf(out, "// TODO enumeration types for enumerated values\n\n")

	for _, cd := range sch.ClassDefs {
		name := cd.Name
		name = strings.Replace(name, " ", "", -1)
		name = strings.Replace(name, "-", "", -1)
		name = strings.Replace(name, "/", "", -1)
		if !unicode.IsLetter([]rune(name)[0]) {
			name = "A" + name
		}

		fmt.Fprintf(out, "type %s struct {\n", name)
		fmt.Fprintf(out, "\tSOPClassUID bool `%s`\n", cd.SOPClassUid)

		for _, mdu := range cd.Modules {
			name = mdu.Name
			name = strings.Replace(name, " ", "", -1)
			name = strings.Replace(name, "-", "", -1)
			name = strings.Replace(name, "/", "", -1)

			if !unicode.IsLetter([]rune(name)[0]) {
				name = "A" + name
			}

			typ := name

			if mdu.Usage == "U" {
				typ = "*" + name
			}

			// TODO what happened here?
			if name == "EnhancedXAXRFImage" {
				name = "//" + name
			}

			fmt.Fprintf(out, "\t%s %s\n", name, typ)
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

		fmt.Fprintf(out, "type %s struct {\n", name)
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
			typ := "[]string"

			switch vrk {
			case dicomtag.VRStringList:
				typ = "string"
			case dicomtag.VRBytes:
				typ = "[]byte"
			case dicomtag.VRUInt16List:
				typ = "uint16"
			case dicomtag.VRUInt32List:
				typ = "uint32"
			case dicomtag.VRInt16List:
				typ = "int16"
			case dicomtag.VRInt32List:
				typ = "int32"
			case dicomtag.VRFloat32List:
				typ = "float32"
			case dicomtag.VRFloat64List:
				typ = "float64"
			case dicomtag.VRSequence:
				typ = name
			case dicomtag.VRItem:
				typ = "[]*dicom.Element"
			case dicomtag.VRTagList:
				typ = "dicomtag.Tag"
			case dicomtag.VRDate:
				typ = "string"
			case dicomtag.VRPixelData:
				typ = "dicom.PixelDataInfo"
			}

			// Any range or sequence just translated into a slice
			if strings.Contains(td.VM, "-") || td.VR[0] == "SQ" {
				typ = "[]" + typ
			}

			// There could be a specific number of required values
			if _, err := strconv.Atoi(td.VM); err == nil && td.VM != "1" {
				typ = fmt.Sprintf("[%s]%s", td.VM, typ)
			}

			// Optional types that aren't already slices are made into pointer types
			//  so that they can be nil. Slices are exempt since their zero value is
			//  already a kind of pointer that can be nil.
			if tgu.Type != "1" && tgu.Type != "2" && !strings.Contains(typ, "[]") {
				typ = "*" + typ
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

			fmt.Fprintf(out, "\t%s %s `tag:\"%s\" vr:\"%s\" vm:\"%s\" deidentify:\"%s\" types:\"%s\"`\n", name, typ, dcmtag, td.VR[0], td.VM, td.Deidentify, audits)
		}
		fmt.Fprintf(out, "}\n\n")
	}
}
