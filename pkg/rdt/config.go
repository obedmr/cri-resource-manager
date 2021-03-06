/*
Copyright 2019 Intel Corporation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rdt

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ghodss/yaml"
)

// ResctrlGroupConfig represents configuration of one CTRL group in the Linux
// resctrl interface
type ResctrlGroupConfig struct {
	L3Schema     L3Schema `json:"l3Schema,omitempty"`
	L3CodeSchema L3Schema `json:"l3CodeSchema,omitempty"`
	L3DataSchema L3Schema `json:"l3DataSchema,omitempty"`
	MBSchema     MBSchema `json:"mbSchema,omitempty"`
}

// SchemaOptions contains the common settings for all resctrl groups
type SchemaOptions struct {
	L3     L3Options `json:"l3,omitempty"`
	L3Code L3Options `json:"l3code,omitempty"`
	L3Data L3Options `json:"l3data,omitempty"`
	MB     MBOptions `json:"mb,omitempty"`
}

// L3 contains the common settings for L3 cache allocation
type L3Options struct {
	Optional bool `json:"optional,omitempty"`
}

// MB contains the common settings for memory bandwidth allocation
type MBOptions struct {
	Optional bool `json:"optional,omitempty"`
}

// L3Schema represents an L3 part of the schemata of a resctrl group
type L3Schema struct {
	Allocations map[uint64]CacheBitmask
}

// MBSchema represents an MB part of the schemata of a resctrl group
type MBSchema struct {
	Allocations map[uint64]uint64
}

type L3SchemaType string

const (
	L3SchemaTypeUnified = ""
	L3SchemaTypeData    = "DATA"
	L3SchemaTypeCode    = "CODE"
)

// IsNil returns true if the schema is empty
func (s *L3Schema) IsNil() bool {
	return s.Allocations == nil
}

// ToStr returns the L3 schema in a format accepted by the Linux kernel
// resctrl (schemata) interface
func (s *L3Schema) ToStr(typ L3SchemaType) string {
	if s.IsNil() {
		return s.DefaultStr(typ)
	}

	schema := "L3" + string(typ) + ":"
	sep := ""

	// We get cache ids but that doesn't matter
	for id, bitmask := range s.Allocations {
		schema += fmt.Sprintf("%s%d=%x", sep, id, bitmask)
		sep = ";"
	}

	return schema + "\n"
}

// DefaultStr returns the L3 default schema
func (s *L3Schema) DefaultStr(typ L3SchemaType) string {
	schema := "L3" + string(typ) + ":"
	sep := ""

	mask := rdtInfo.l3FullMask()

	for _, id := range rdtInfo.cacheIds {
		// Set all to full mask (i.e. 100%)
		schema += fmt.Sprintf("%s%d=%x", sep, id, mask)
		sep = ";"
	}

	return schema + "\n"
}

func (s *L3Schema) UnmarshalJSON(b []byte) error {
	var allocations map[string]CacheBitmask

	err := yaml.Unmarshal(b, &allocations)
	if err != nil {
		return err
	}

	s.Allocations = map[uint64]CacheBitmask{}

	// Set default allocations
	defaultMask, ok := allocations["all"]
	if !ok {
		// Set to 100% if "all" is not specified
		defaultMask = CacheBitmask(rdtInfo.l3FullMask())
	}
	delete(allocations, "all")

	for _, i := range rdtInfo.cacheIds {
		s.Allocations[i] = defaultMask
	}

	// Parse per-cacheId allocations
	for key, mask := range allocations {
		ids, err := listStrToArray(key)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, ok := s.Allocations[uint64(id)]; ok {
				s.Allocations[uint64(id)] = mask
			}
		}
	}

	return nil
}

// IsNil returns true if the schema is empty
func (s *MBSchema) IsNil() bool {
	return s.Allocations == nil
}

// ToStr returns the MB schema in a format accepted by the Linux kernel
// resctrl (schemata) interface
func (s *MBSchema) ToStr() string {
	if s.IsNil() {
		return s.DefaultStr()
	}

	schema := "MB:"
	sep := ""

	// We get cache ids but that doesn't matter
	for id, percentage := range s.Allocations {
		schema += fmt.Sprintf("%s%d=%d", sep, id, percentage)
		sep = ";"
	}

	return schema + "\n"
}

// DefaultStr returns the L3 default schema
func (s *MBSchema) DefaultStr() string {
	schema := "MB:"
	sep := ""

	for _, id := range rdtInfo.cacheIds {
		// Set all to 100 percent
		schema += fmt.Sprintf("%s%d=100", sep, id)
		sep = ";"
	}

	return schema + "\n"
}

func (s *MBSchema) UnmarshalJSON(b []byte) error {
	var allocations map[string]uint64

	err := yaml.Unmarshal(b, &allocations)
	if err != nil {
		return err
	}

	s.Allocations = map[uint64]uint64{}

	// Set default allocations
	defaultVal, ok := allocations["all"]
	if !ok {
		// Set to 100 if "all" is not specified
		defaultVal = 100
	}
	delete(allocations, "all")

	for _, i := range rdtInfo.cacheIds {
		s.Allocations[i] = defaultVal
	}

	// Parse per-cacheId allocations
	for key, val := range allocations {
		ids, err := listStrToArray(key)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, ok := s.Allocations[uint64(id)]; ok {
				s.Allocations[uint64(id)] = val
			}
		}
	}

	return nil
}

// listStrToArray parses a string containing a human-readable list of numbers
// into an integer array
func listStrToArray(str string) ([]int, error) {
	a := []int{}

	// Empty list
	if len(str) == 0 {
		return a, nil
	}

	ranges := strings.Split(str, ",")
	for _, ran := range ranges {
		split := strings.SplitN(ran, "-", 2)

		// We limit to 8 bits in order to avoid accidental super long slices
		num, err := strconv.ParseInt(split[0], 10, 8)
		if err != nil {
			return a, rdtError("invalid integer %q: %v", str, err)
		}

		if len(split) == 1 {
			a = append(a, int(num))
		} else {
			endNum, err := strconv.ParseInt(split[1], 10, 8)
			if err != nil {
				return a, rdtError("invalid integer in range %q: %v", str, err)
			}
			if endNum <= num {
				return a, rdtError("invalid integer range %q in %q", ran, str)
			}
			for i := num; i <= endNum; i++ {
				a = append(a, int(i))
			}
		}
	}
	sort.Ints(a)
	return a, nil
}
