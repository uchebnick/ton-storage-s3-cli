package ton

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

var KnownProviders = []string{
	"EQDJlWIX-7HcN-7RqDhFhpuCfbHLruOMJ2p4hKKr_AN7_R9S",
}

func (s *Service) FindRandomProvider(ctx context.Context, exclude []string) (string, error) {

	excludedMap := make(map[string]bool)
	for _, addr := range exclude {
		excludedMap[addr] = true
	}

	var candidates []string
	for _, provider := range KnownProviders {

		if !excludedMap[provider] {
			candidates = append(candidates, provider)
		}
	}

	if len(candidates) == 0 {
		return "", errors.New("no available providers left (all known providers are already used)")
	}

	rand.Seed(time.Now().UnixNano())
	randomIndex := rand.Intn(len(candidates))

	return candidates[randomIndex], nil
}
