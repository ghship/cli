package main

import (
	"encoding/json"
	"fmt"
)

type npmPack struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Filename string `json:"filename"`
	Files    []struct {
		Path string `json:"path"`
	} `json:"files"`
}

func parseNpmPack(data []byte, name string) (npmPack, error) {
	var list []npmPack
	if json.Unmarshal(data, &list) == nil && len(list) == 1 && list[0].Name == name {
		return list[0], nil
	}
	var keyed map[string]npmPack
	if json.Unmarshal(data, &keyed) == nil && len(keyed) == 1 {
		if item, ok := keyed[name]; ok && item.Name == name {
			return item, nil
		}
	}
	return npmPack{}, fmt.Errorf("unexpected npm pack result for %s", name)
}
