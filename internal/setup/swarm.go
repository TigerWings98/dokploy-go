package setup

import "github.com/docker/docker/api/types/swarm"

func swarmInitRequest() swarm.InitRequest {
	return swarm.InitRequest{
		ListenAddr:    "0.0.0.0:2377",
		AdvertiseAddr: "127.0.0.1",
	}
}
