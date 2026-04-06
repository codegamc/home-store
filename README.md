# Home Store

Home Store is an "object storage" server designed for home-lab use. It is tested to be compatible with the AWS S3 API.

## Goals

The goal of this server is for easy and effortless deployment of a correct object storage API for users to self-host on "Home Lab" servers on their LAN. This means that some of decisions made for this may not match "production" servers that would be used in a commercial context, but make it easier and lower-maintainence for an individual. The representative user of this is an individual who would want to run an object storage API on their NAS, maybe alongside NFS, so they can self host software that depends on object storage.

## Non-Goals

This software is not designed to be run in a commercial setting. While it strives for correctness and data integrity, it may make implementation tradeoffs aimed at it's core goal that are sub-optimal for commercial settings, such as around limitations around supported load or scalability.

## Running

TODO - The goal is to support a simple binary, docker, and synology package

## Backing Data

TODO - Currently, locally on file system, goal also to support NFS...?

## API Coverage

Here is the status and coverage of core object storage APIs...

TODO - Table here.

## Development

TODO - This is going to be a single-binary server written in Golang.

## Testing

TODO - We want to test against a variety of client libraries for correctness.

TODO - We want to have CI/CD running to automate testing.