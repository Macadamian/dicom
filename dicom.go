package dicom

import "encoding/json"
import "fmt"
import "github.com/macadamian/dicom/dicom2019b"
import "github.com/gradienthealth/dicom"
import "github.com/gradienthealth/dicom/dicomtag"
import "os"
import "reflect"

func main() {
	inst := dicom2019b.MRImageStorage{}

	inFilePath := "/Users/cmcgee/Downloads/4151021e958b4d09bbd83d6ec75f216c/UnknownStudy/MR-33/MR000000.dcm"

	st, err := os.Stat(inFilePath)
	if err != nil {
		panic(err)
	}
	in, err := os.Open(inFilePath)
	if err != nil {
		panic(err)
	}
	p, err := dicom.NewParser(in, st.Size(), nil)
	if err != nil {
		panic(err)
	}
	ds, err := p.Parse(dicom.ParseOptions{})
	if err != nil {
		panic(err)
	}

	err = Unmarshal(ds, &inst)
	if err != nil {
		panic(err)
	}

	inst.ImagePixel.PixelData = nil
	out, _ := json.MarshalIndent(inst, "", "\t")
	fmt.Printf("%s\n", string(out))
}

// Unmarshal a DICOM dataset into a provided Go value. Note that in most cases
// you will want this value to be a pointer to a storage class from one of the
// sub-packages, such as dicom2019b. For example, if the dataset is an MRI image
// instance then you can use dicom2019b MRImageStorage.
//
// This unmarshaler is expecting that the SOPClassUID matches the one declared on the
// tag of a special SOPClassUID struct property to avoid mismatches on the types. The
// shape of the struct should look as follows:
//   type MyStorage struct {
//     SOPClassUID bool `1.2.3.3.44....`
//     ModuleA ModuleA
//     ModuleB *ModuleB
//   }
//
//   type ModuleA struct {
//     aKeyword string `tag:"(3006,0082)" vr:"IS" vm:"1" deidentify:"" types:"[{ModuleA,(3006,0082)}]"`
//     aSequence Sequence `tag:"(1234,1234)" vr:"SQ" vm:"1" deidentify:"" types:"[{ModuleA,(1234,1234)}]"`
//   }
//
//   type Sequence struct {
//     ...
//   }
//
// Note that any extra DICOM tags that don't fit within the schema structure of the storage and
// related types are ignored and remain in the original dataset along with all of the other tags
// that did match.
func Unmarshal(ds *dicom.DataSet, v interface{}) error {
	pt := reflect.TypeOf(v)
	if pt.Kind() != reflect.Ptr {
		return fmt.Errorf("Must provide a pointer to a storage value to do anything meaningful")
	}

	pv := reflect.ValueOf(v)
	scv := pv.Elem()
	sct := scv.Type()

	if sct.Kind() != reflect.Struct {
		return fmt.Errorf("Must provide a pointer to a storage class struct to do anything meaningful")
	}

	soif, ok := sct.FieldByName("SOPClassUID")

	if !ok {
		return fmt.Errorf("Provided struct is not a storage class struct. It is missing the SOPInstanceUID property")
	}

	expectedSOPClass := soif.Tag

	sce, err := ds.FindElementByTag(dicomtag.SOPClassUID)
	if err != nil {
		return err
	}

	if len(sce.Value) != 1 || sce.Value[0] == expectedSOPClass {
		return fmt.Errorf("Expected Storage Class UID to be %s, but was %v.\n", expectedSOPClass, sce)
	}

	for _, e := range ds.Elements {
		hit := false
		modfn := sct.NumField()
		for i := 0 ; i < modfn; i++ {
			modf := sct.Field(i)
			modt := modf.Type

			var modv reflect.Value
			addModule := func() {
				// Default does nothing since the module is already composite
			}

			if modt.Kind() == reflect.Ptr {
				modt = modt.Elem()
				modv = reflect.New(modt)
				addModule = func() {
					scv.Field(i).Set(modv)
					modv = modv.Elem()
				}
			} else {
				modv = scv.Field(i)
			}

			if modt.Kind() != reflect.Struct {
				continue
			}
			
			fn := modt.NumField()
			for j := 0; j < fn; j++ {
				tagf := modt.Field(j)

				if tagf.Tag.Get("tag") == e.Tag.String() {
					// Add the module if needed
					addModule()
					addModule = func() {}

					tagv := modv.Field(j)

					//fmt.Printf("Assign this value: %d %+v to %v\n", len(e.Value), e.Value, tagf)

					if !modv.Field(j).CanSet() {
						return fmt.Errorf("Can't set %v in %v\n", modt.Name(), modt.Field(j))
					}

					// Simple individual assignment
					if len(e.Value) == 1 && tagf.Type == reflect.TypeOf(e.Value[0]) {
						tagv.Set(reflect.ValueOf(e.Value[0]))
					} else if len(e.Value) == 1 && tagf.Type.Kind() == reflect.Ptr && tagf.Type.Elem() == reflect.TypeOf(e.Value[0]) {
						// Temporary slice of the correct type so that we can get an address of the value
						s := reflect.MakeSlice(reflect.SliceOf(reflect.TypeOf(e.Value[0])), 0, 1)
						s = reflect.Append(s, reflect.ValueOf(e.Value[0]))
						tagv.Set(s.Index(0).Addr())
					} else if len(e.Value) == 0 {
						// This is fine, nothing to assign
					} else if tagf.Type.Kind() == reflect.Array {
						for k := 0; k < tagf.Type.Len() && k < len(e.Value); k++ {
							tagv.Index(k).Set(reflect.ValueOf(e.Value[k]))
						}
					} else if tagf.Type.Kind() == reflect.Ptr && tagf.Type.Elem().Kind() == reflect.Array {
						av := reflect.New(reflect.ArrayOf(tagf.Type.Elem().Len(), tagf.Type.Elem().Elem()))
						for k := 0; k < tagf.Type.Elem().Len() && k < len(e.Value); k++ {
							av.Elem().Index(k).Set(reflect.ValueOf(e.Value[k]))
						}
						tagv.Set(av)
					} else if tagf.Type.Kind() == reflect.Slice && tagf.Type.Elem().Kind() != reflect.Struct {
						sv := reflect.MakeSlice(reflect.SliceOf(tagf.Type.Elem()), 0, 0)
						for _, v := range e.Value {
							sv = reflect.Append(sv, reflect.ValueOf(v))
						}
						tagv.Set(sv)
					} else {
						fmt.Printf("FIELD: %+v VALUE: %+v\n", tagf.Type, reflect.TypeOf(e.Value))
						fmt.Printf("Could not assign %+v to %+v\n", e, modt.Field(j))
					}
					
					if hit {
						fmt.Printf("DUPLICATED: %s\n", e)
					}
					hit = true
				}
			}
		}

		if !hit {
			fmt.Printf("MISS: %v\n", e)
		}
	}

	return nil
}