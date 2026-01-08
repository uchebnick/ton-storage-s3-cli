package ton

import (
	"context"
	"strings"
	"math/rand"
	"time"
)

var KnownProviders = []string{
	"95b42da4168733cbed792d2baf3c40facb6ebc432b04d402dc764110cbf9d6fb",
	"3d11c40a1fe54d2b69cf1f8280ce7294ae4c1c7c98ddcc73653a3cf35a1b65af",
	"99dedb19e91aded4aacc8427252a14d2ed7ed256fa95365250d13d1e0c0c8ea1",
}


func (s *Service) FindRandomProvider(ctx context.Context, exclude []string) (string, error) {

	excludedMap := make(map[string]bool)
	for _, addr := range exclude {
		if len(addr) > 64 && strings.Contains(addr, ":") {
			parts := strings.Split(addr, ":")
			if len(parts) == 2 {
				excludedMap[parts[1]] = true
			}
		} else {
			excludedMap[addr] = true
		}
	}

	var candidates []string
	for _, provider := range KnownProviders {
		if !excludedMap[provider] {
			candidates = append(candidates, provider)
		}
	}

	if len(candidates) == 0 {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		return KnownProviders[rng.Intn(len(KnownProviders))], nil
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return candidates[rng.Intn(len(candidates))], nil
}