package main

import (
	"encoding/json"
	"fmt"
)

func fromDB[T any](m map[string]any) (T, error) {
	b, err := json.Marshal(m)
	if err != nil {
		var t T
		return t, fmt.Errorf("failed to marshal json: %w", err)
	}

	var res T
	if err := json.Unmarshal(b, &res); err != nil {
		var t T
		return t, fmt.Errorf("failed to unmarshal json: %w", err)
	}

	return res, nil
}

func toDB[T any](m T) (map[string]any, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json: %w", err)
	}

	var res map[string]any
	if err := json.Unmarshal(b, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal json: %w", err)
	}

	delete(res, "id")
	delete(res, "station_type")

	return res, nil
}
