# The purpose of the script is to build a golang string constant containing the
# YAML code for the Match CRD. This is needed to auto generate the
# JSONSchemaProps for Match.
# It will parse the YAML for the Match CRD, found in $CRD_FILE, and output to
# $GO_FILE. For controller-gen to generate the CRD, we must include the metadata
# and typemeta fields. Since we don't want these fields to exist on the real
# Match CRD, we embed the Match type in a dummy type that has the
# metadata/typemeta fields, and then parse out these unwanted fields.

GO_FILE="./pkg/target/matchcrd_constant.go"
SRC_FILE="./pkg/mutation/match/match_types.go"
CRD_FILE="./config/crd/bases/match.gatekeeper.sh_matchcrd.yaml"

cat << EOF > ${GO_FILE}
package target

// DO NOT MODIFY THIS FILE DIRECTLY!
// This file is generated from $SRC_FILE via "make manifests".

const matchYAML = \`
EOF

# Delete apiVersion block, adjust indentation to un-embed the match field, escape backticks
start=$(cat ${CRD_FILE} | grep -n "description: DummyCRD" | cut -d: -f1)
end=$(cat ${CRD_FILE} | grep -n "embeddedMatch:" | cut -d: -f1)
cat ${CRD_FILE} | sed "${end},$ s/  //" | sed "${start},${end}d" | sed "s/\`/\`+\"\`\"+\`/g" >> ${GO_FILE}

# Delete the 'kind:' and 'metadataDummy:' blocks at the end. This assumes the metadataDummy
# block is immediately after the kind block, and the metadataDummy block contains only
# one line (type: object)
start=$(cat ${GO_FILE} | grep -n -E "kind:$" | cut -d: -f1)
end=$(cat ${GO_FILE} | grep -n -E "metadataDummy:$" | cut -d: -f1)
end=$((end+1))
sed -i "${start},${end}d" ${GO_FILE}

echo "\`" >> ${GO_FILE}

gofmt -w -l ${GO_FILE}