package ton

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

var KnownProviders = []string{
	"c9956217fbb1dc37eed1a83845869b827db1cbaee38c276a7884a2abfc037bfd",
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
