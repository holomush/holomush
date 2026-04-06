// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"fmt"
	"strconv"
	"strings"
)

const brokerPrefix = "broker:"

// ParseBrokerServices parses the required_services map from InitRequest
// into a map of service name to broker ID. Each value must have the format
// "broker:<uint32>".
func ParseBrokerServices(services map[string]string) (map[string]uint32, error) {
	result := make(map[string]uint32, len(services))
	for name, value := range services {
		if !strings.HasPrefix(value, brokerPrefix) {
			return nil, fmt.Errorf("service %q: expected %q prefix, got %q", name, brokerPrefix, value)
		}
		idStr := strings.TrimPrefix(value, brokerPrefix)
		id, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("service %q: invalid broker ID %q: %w", name, idStr, err)
		}
		result[name] = uint32(id)
	}
	return result, nil
}
