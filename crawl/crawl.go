package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/gradienthealth/dicom/dicomtag"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

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

//// XML Parsing

type Node struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Content string     `xml:",innerxml"`
	Nodes   []Node     `xml:",any"`
}

type NodeDict struct {
	Dict map[string]*Node
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsGraphic(r) {
			return r
		}
		return -1
	}, s)
}

func main() {
	versions := []string{"2013"}

	for _, year := range []string{"2014", "2015", "2016", "2017", "2019"} {
		for _, rev := range []string{"a", "b", "c"} {
			versions = append(versions, fmt.Sprintf("%s%s", year, rev))
		}
	}

	for _, version := range versions {
		extractDicom(version)
	}
}

func extractDicom(version string) {
	linkLookup := map[string]*NodeDict{}

	for part := 1; part < 22; part++ {
		url := ""
		if version == "2013" {
			url = fmt.Sprintf("http://dicom.nema.org/dicom/%s/source/docbook/part%02d/part%02d.xml", version, part, part)
		} else {
			url = fmt.Sprintf("http://dicom.nema.org/medical/dicom/%s/source/docbook/part%02d/part%02d.xml", version, part, part)
		}

		fmt.Fprintf(os.Stderr, "Fetching %s\n", url)
		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			continue
		}

		decoder := xml.NewDecoder(resp.Body)
		var n Node

		if err = decoder.Decode(&n); err != nil {
			panic(err)
		}

		idMap := map[string]*Node{}
		walkNode([]Node{n}, func(n Node) bool {
			// Find the ID, if any
			id := ""
			for _, a := range n.Attrs {
				if a.Name.Space == "http://www.w3.org/XML/1998/namespace" && a.Name.Local == "id" {
					id = a.Value
					break
				}
			}

			if id != "" {
				if _, ok := idMap[id]; ok {
					fmt.Fprintf(os.Stderr, "XML element already in the ID map")
				} else {
					idMap[id] = &n
				}
			}

			return true
		})

		docId := ""
		for _, a := range n.Attrs {
			if a.Name.Space == "http://www.w3.org/XML/1998/namespace" && a.Name.Local == "id" {
				docId = a.Value
				break
			}
		}

		if docId == "" {
			fmt.Fprintf(os.Stderr, "Document has no id\n")
			continue
		}

		linkLookup[docId] = &NodeDict{idMap}
	}

	part4 := linkLookup["PS3.4"]
	stdSopClsSctn := part4.Dict["sect_B.5"]
	stdSopClsTbl := findNodeByType(stdSopClsSctn, "http://docbook.org/ns/docbook", "table")

	sopClasses := []*ClassDef{}

	modules := map[string]*ModuleDef{}

	walkNode([]Node{*stdSopClsTbl}, func(n Node) bool {
		if n.XMLName.Space == "http://docbook.org/ns/docbook" && n.XMLName.Local == "tr" {
			cn, spu, sp := n.Nodes[0], n.Nodes[1], n.Nodes[2]

			if cn.XMLName.Space != "http://docbook.org/ns/docbook" || cn.XMLName.Local != "td" {
				return true
			}

			ol := findNodeByType(&sp, "http://docbook.org/ns/docbook", "olink")
			sp = *ol

			sopClassName := cn.Nodes[0].Content
			sopClassUid := sanitize(spu.Nodes[0].Content)

			sopClass := ClassDef{Name: sopClassName, SOPClassUid: sopClassUid, Modules: []ModuleUsage{}}
			sopClasses = append(sopClasses, &sopClass)

			doc := sp.Attrs[0].Value
			sect := sp.Attrs[1].Value

			part := linkLookup[doc]

			var modtbl *Node
			i := 1
			suffix := ""

			// Heuristics to identify the IOD modules table
			for {
				modsctn := part.Dict[sect+suffix]
				if modsctn == nil {
					return true
				}

				modtbl = findNodeByType(modsctn, "http://docbook.org/ns/docbook", "table")

				if modtbl != nil && strings.HasSuffix(modtbl.Nodes[0].Content, " IOD Modules") {
					break
				}

				suffix = "." + strconv.Itoa(i)
				i++
			}

			walkNode([]Node{*modtbl}, func(n Node) bool {
				if n.XMLName.Space == "http://docbook.org/ns/docbook" && n.XMLName.Local == "tr" {
					if len(n.Nodes) != 4 && len(n.Nodes) != 3 {
						return true
					}

					l := len(n.Nodes)
					mdl, ref, usg := n.Nodes[l-3], n.Nodes[l-2], n.Nodes[l-1]

					if mdl.XMLName.Space != "http://docbook.org/ns/docbook" || mdl.XMLName.Local != "td" {
						return true
					}

					u := strings.Split(usg.Nodes[0].Content, " - ")[0]

					m := ModuleUsage{Name: mdl.Nodes[0].Content, Usage: u}
					sopClass.Modules = append(sopClass.Modules, m)

					r := findNodeByType(&ref, "http://docbook.org/ns/docbook", "xref")

					// Module definition is already recorded
					if _, ok := modules[m.Name]; ok {
						return true
					}

					// Assumption that the reference is always same-document
					mdlsect := part.Dict[r.Attrs[0].Value]
					mdlattrtbl := findNodeByType(mdlsect, "http://docbook.org/ns/docbook", "table")

					if mdlattrtbl != nil && strings.HasSuffix(mdlattrtbl.Nodes[0].Content, "Module Attributes") {
						mdldef := ModuleDef{}

						var attrHandler func(Node, int) bool
						parents := []string{}

						attrHandler = func(n Node, reflevel int) bool {
							if n.XMLName.Space == "http://docbook.org/ns/docbook" && n.XMLName.Local == "tr" {
								// Compute the level within this scope that this tag is located
								var level int
								if len(n.Nodes) > 0 {
									level = strings.Count(n.Nodes[0].Content, "&gt;") + reflevel
								}

								// Included macro
								if len(n.Nodes) == 1 || len(n.Nodes) == 2 {
									mcr := findNodeByType(&n, "http://docbook.org/ns/docbook", "xref")
									if mcr != nil {
										if level > 3 && mcr.Attrs[0].Value == "table_C.17-6" {
											// Special case of (0040, a730) that allows infinite recursion
											return true
										}
										mcrsect := part.Dict[mcr.Attrs[0].Value]
										mcrattrtbl := findNodeByType(mcrsect, "http://docbook.org/ns/docbook", "table")
										if mcrattrtbl != nil {
											walkNode([]Node{*mcrattrtbl}, func(n Node) bool { return attrHandler(n, level) })
										}
									}
								}

								if len(n.Nodes) != 4 || (len(n.Nodes) > 0 && n.Nodes[0].XMLName.Local != "td") {
									return true
								}

								tg, tp := n.Nodes[1], n.Nodes[2]

								tn := tg.Nodes[0]
								if len(tn.Nodes) != 0 {
									tn = tn.Nodes[0]
								}

								for _, t := range parseTagPattern(tn.Content) {
									if len(parents) <= level {
										parents = append(parents, t.String())
									} else {
										parents[level] = t.String()

										// Potentially resize the parents slice
										parents = parents[:level+1]
									}

									tdef := TagUsage{}
									tdef.Type = tp.Nodes[0].Content
									tdef.Path = append([]string{}, parents...)
									mdldef.Tags = append(mdldef.Tags, tdef)
								}
							}
							return true
						}

						walkNode([]Node{*mdlattrtbl}, func(n Node) bool { return attrHandler(n, 0) })

						modules[m.Name] = &mdldef
					}
				}

				return true
			})
		}
		return true
	})

	tagdefs := map[string]*TagDef{}

	part6 := linkLookup["PS3.6"]
	dataElementsSctn := part6.Dict["table_6-1"]
	walkNode([]Node{*dataElementsSctn}, func(n Node) bool {
		if n.XMLName.Space == "http://docbook.org/ns/docbook" && n.XMLName.Local == "tr" {
			if len(n.Nodes) != 6 {
				return true
			}

			tag, keyword, vr, vm := n.Nodes[0], n.Nodes[2], n.Nodes[3], n.Nodes[4]

			if tag.XMLName.Space != "http://docbook.org/ns/docbook" || tag.XMLName.Local != "td" {
				return true
			}

			// Skip retired tags
			if strings.Contains(n.Nodes[5].Content, "RET") {
				return true
			}

			v := ""
			getVal := func(n Node) bool {
				if len(n.Nodes) == 0 {
					v = n.Content
				}
				return true
			}

			walkNode([]Node{tag}, getVal)
			tagstr := v

			walkNode([]Node{keyword}, getVal)
			keywordstr := sanitize(v)

			walkNode([]Node{vr}, getVal)
			vrstr := v

			walkNode([]Node{vm}, getVal)
			vmstr := v

			tagdef := TagDef{}

			for _, t := range parseTagPattern(tagstr) {
				tagdef.Keyword = keywordstr
				tagdef.VR = []string{}

				for _, r := range strings.Split(vrstr, " or ") {
					tagdef.VR = append(tagdef.VR, r)
				}

				tagdef.VM = vmstr
				tagdefs[t.String()] = &tagdef
			}
		}

		return true
	})

	part15 := linkLookup["PS3.15"]
	confidProfileAttrTbl := part15.Dict["table_E.1-1"]

	walkNode([]Node{*confidProfileAttrTbl}, func(n Node) bool {
		if n.XMLName.Space == "http://docbook.org/ns/docbook" && n.XMLName.Local == "tr" {
			if len(n.Nodes) < 5 {
				return true
			}

			tag, basicprof := n.Nodes[1], n.Nodes[4]

			if tag.XMLName.Space != "http://docbook.org/ns/docbook" || tag.XMLName.Local != "td" {
				return true
			}

			v := ""
			getVal := func(n Node) bool {
				if len(n.Nodes) == 0 {
					v = n.Content
				}
				return true
			}

			walkNode([]Node{tag}, getVal)
			tagstr := v

			walkNode([]Node{basicprof}, getVal)
			basicprofstr := v

			for _, t := range parseTagPattern(tagstr) {
				td := tagdefs[t.String()]
				if td == nil {
					// Likely this is a retired tag, just skip
					return true
				}

				td.Deidentify = basicprofstr
			}
		}

		return true
	})

    os.MkdirAll(fmt.Sprintf("../dicom%sdata", version), 0700)

	out, err := os.Create(fmt.Sprintf("../dicom%sdata/dicom-%s.go", version, version))
	if err != nil {
		panic(err)
	}
	defer out.Close()

	fmt.Fprintf(out, "// Schema data for DICOM version %s\n", version)
	fmt.Fprintf(out, "// Date Assembled: %s\n", time.Now().Format(time.UnixDate))
	fmt.Fprintf(out, "package dicom%sdata\n", version)
	fmt.Fprintf(out, `
// Unmarshal this string into a github.com/macadamian/dicom SchemaDef using encoding/json package.
// You can assign an empty string here afterwards to free memory.
var SchemaStr =`)

	fmt.Fprintf(out, "`\n")

	od := SchemaDef{ClassDefs: sopClasses, TagDefs: tagdefs, ModuleDefs: modules}
	b, err := json.MarshalIndent(od, "", "\t")
	if err != nil {
		panic(err)
	}
	out.Write(b)

	fmt.Fprintf(out, "`\n")

	fmt.Printf("DONE dicom-%s.go\n", version)
}

func walkNode(nodes []Node, f func(Node) bool) {
	for _, n := range nodes {
		if f(n) {
			walkNode(n.Nodes, f)
		}
	}
}

func findNodeByType(node *Node, space, local string) *Node {
	var match *Node

	walkNode([]Node{*node}, func(n Node) bool {
		if match == nil && n.XMLName.Space == space && n.XMLName.Local == local {
			match = &n
		}

		return true
	})

	return match
}

func parseTagPattern(pattern string) []dicomtag.Tag {
	pattern = strings.TrimSpace(pattern)

	if !strings.Contains(pattern, ",") {
		fmt.Printf("BAD TAG: %s\n", pattern)
		return []dicomtag.Tag{}
	}

	pieces := strings.Split(pattern, ",")

	if len(pieces) != 2 {
		fmt.Printf("BAD TAG: %s\n", pattern)
		return []dicomtag.Tag{}
	}

	g := pieces[0]
	el := pieces[1]

	if strings.HasPrefix(g, "(") {
		g = g[1:]
	}

	if strings.HasSuffix(el, ")") {
		el = el[:len(el)-1]
	}

	g = strings.TrimSpace(g)
	el = strings.TrimSpace(el)

	if g == "50xx" {
		// Curve patterns have been retired for a long time (2004)
		return []dicomtag.Tag{}
	}

	gs := []string{g}

	// Special repeating pattern for overlay group
	if g == "60xx" {
		gs = []string{}

		for i := 0; i < 16; i++ {
			for j := 0; j < 16; j++ {
				gs = append(gs, fmt.Sprintf("60%X%X", i, j))
			}
		}
	}

	tags := []dicomtag.Tag{}

	for _, gr := range gs {
		group, err := strconv.ParseUint(gr, 16, 16)
		if err != nil {
			fmt.Printf("BAD TAG: %s\n", pattern)
			return []dicomtag.Tag{}
		}
		element, err := strconv.ParseUint(el, 16, 16)
		if err != nil {
			fmt.Printf("BAD TAG: %s\n", pattern)
			return []dicomtag.Tag{}
		}

		t := dicomtag.Tag{uint16(group), uint16(element)}
		tags = append(tags, t)
	}

	return tags
}
