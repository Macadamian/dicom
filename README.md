# DICOM Schema

Building on top of the marshaling/unmarshaling offered by the [Go DICOM](https://github.com/gradienthealth/dicom)
package there should be a way to reason about what makes a DICOM specification compliant. This package takes
information from the different publicly available versions of the spec and encodes them in forms that are useful
in the Go language.

In this package you will find sub-packages that contain JSON representations of the spec from different
versions (eg. dicom2013data). These can be unmarshaled into a SchemaDef (type is found in this package) for information
about the SOP Classes, modules and tags from that version of the specification. These can be useful for validating
DICOM instances as an example or introspecting a tag to determine what value represetnations it should contain. 
Because the data is in the form of Go code you can statically compile this information into your Go program
without having to bring along extra artifacts. The different versions of the spec are in different packages so that
only the versions you require are linked into your program.

An early experimental effort was made to generate type structures (and extra metadata) to represent well-formed
DICOM in terms of Go values and types. Large classes of programming errors could be captured early as
compiler errors instead of through integration and interoperability testing much later on. Other classes
can be detected through a validation step performed before marshaling the DICOM. The result of this
experiment can be found in the dicom2019b sub-package with some yet to be resolved problems. The hope is that
community feedback might help and improve it so that it is useful.

Sub-packages:
* crawl - DICOM specification crawler that generates the dicomYYYYRdata packages
* codegen - Experimental code generator that generates the dicom2019b package
* dicomYYYYRdata - Packages with linkable, unmarshalable JSON representations of spec information
* dicom2019b - Experimental Go type representation of the SOP Classes from the DICOM spec