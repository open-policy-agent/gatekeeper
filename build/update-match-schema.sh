# This script builds a golang string constant containing the YAML code for the
# Match CRD. This is needed to auto generate the JSONSchemaProps for Match. It
# will parse the YAML for the Match CRD, found in $CRD_FILE, and output to
# $GO_FILE.

GO_FILE="./pkg/target/matchcrd_constant.go"
SRC_FILE="./pkg/mutation/match/match_types.go"
CRD_FILE="./config/crd/bases/match.gatekeeper.sh_matchcrd.yaml"

cat << EOF > ${GO_FILE}
package target

// DO NOT MODIFY THIS FILE DIRECTLY!
// This file is generated from $SRC_FILE via "make manifests".

const matchYAML = \`
EOF

# Escape backticks in the yaml, add terminating backtick
cat ${CRD_FILE} | sed "s/\`/\`+\"\`\"+\`/g" >> ${GO_FILE}
echo "\`" >> ${GO_FILE}

gofmt -w -l ${GO_FILE}
