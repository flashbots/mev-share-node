# mev-share-node

> :warning: **DISCLAIMER**: The Flashbots mev-share node is under active development. This code is in beta and may have performance issues or other vulnerabilities. While we welcome input in the form of pull requests or issues, Flashbots does not commit to particular SLAs around responding or maintaining this repository.

[![Test status](https://github.com/flashbots/go-template/workflows/Checks/badge.svg)](https://github.com/flashbots/go-template/actions?query=workflow%3A%22Checks%22)

This repo is an implementation of the MEV-Share API.
It handles tasks such as bundle simulation, matching, and hint extraction.

## Live Deployments

### Mainnet

- Bundle Submission: [relay.flashbots.net](https://relay.flashbots.net)
- Hint Stream: [mev-share.flashbots.net](https://mev-share.flashbots.net)

### Goerli

- Bundle Submission: [relay-goerli.flashbots.net](https://relay-goerli.flashbots.net)
- Hint Stream: [mev-share-goerli.flashbots.net](https://mev-share-goerli.flashbots.net)

## Supported Methods

More detailed documentation can be found in the [Flashbots documentation](https://docs.flashbots.net/flashbots-mev-share/overview) 
and the [mev-share repository](https://github.com/flashbots/mev-share).

### `mev_sendBundle`

This method is utilized for submitting bundles to the relay.
It takes in a bundle and provides a bundle hash as a return value.

The detailed structure of bundles is explained in the [Flashbots documentation](https://docs.flashbots.net/flashbots-mev-share/searchers/understanding-bundles).

Node processes the bundle in the following manner:

1. Validates the structure of the bundle.
2. If the bundle is unmatched, i.e., if a hash element exists in the body, the node searches for a corresponding bundle with the same hash in the database. If a match is found and the target bundle can be matched, the hash is replaced with the bundle body. If not, the bundle is rejected.
3. Adds the bundle to the simulation queue.
4. Simulates the bundle when the block preceding its target block is reached.
5. If the `privacy.hint` of the bundle is set, relevant hints are extracted and added to the Redis channel. A separate service will then stream it over the SEE endpoint.
6. Sends the bundle to the builders specified in the `privacy.builders` field of the bundle. By default, the Flashbots builder is assumed.

### `mev_simBundle`

This method has similar arguments to `mev_sendBundle`,
but instead of submitting a bundle to the relay, it returns a simulation result.
Only fully matched bundles can be simulated.

# Running the service

## Dependencies

- Redis: Used for hint streaming and priority queue.
- Postgres: Used for storing bundles and historical hints.

## Configuration

The full list of configuration options can be found in [cmd/node/main.go](cmd/node/main.go).

## Running Locally

```bash
docker-compose up # start services: redis and postgres

# apply migration
for file in sql/*.sql; do psql "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable" -f $file; done

# get flashbots/builder, see /local-builder/devnet/README.md
cd ..
git clone https://github.com/flashbots/builder
cd builder
make
./local-builder/devnet/devnet run

# run node
make && ./build/node
```

# Maintainers

- [@dvush](https://github.com/dvush)

# Contributing

[Flashbots](https://flashbots.net) is a research and development collective working on mitigating the negative externalities of decentralized economies. We contribute with the larger free software community to illuminate the dark forest.

You are welcome here <3.

- If you have a question, feedback or a bug report for this project, please [open a new Issue](https://github.com/flashbots/mev-share-node/issues).
- If you would like to contribute with code, check the [CONTRIBUTING file](CONTRIBUTING.md) for further info about the development environment.
- We just ask you to be nice. Read our [code of conduct](CODE_OF_CONDUCT.md).

# Security

If you find a security vulnerability on this project or any other initiative related to Flashbots, please let us know sending an email to security@flashbots.net.

# License

The code in this project is free software under the [AGPL License version 3 or later](LICENSE).

---

Made with ☀️ by the ⚡🤖 collective.
