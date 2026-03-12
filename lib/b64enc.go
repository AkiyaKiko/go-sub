package lib

import (
	"encoding/base64"
	"strings"
)

func NodeSplice(nodes []string) string {
	var result strings.Builder
	for i, node := range nodes {
		if i > 0 {
			result.WriteString("\n")
		}
		result.WriteString(node)
	}
	return result.String()
}

func B64Encode(str string) string {
	return base64.StdEncoding.EncodeToString([]byte(str))
}

func GetSubs(nodes_raw []string) string {
	nodes_splited := NodeSplice(nodes_raw)
	return B64Encode(nodes_splited)
}

func GetB64FromLib() (string, error) {
	nodes, err := GetNodes()
	if err != nil {
		return "", err
	}
	return GetSubs(nodes), nil
}
