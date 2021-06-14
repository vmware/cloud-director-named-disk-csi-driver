# Contributing Guidelines

The **cloud-director-named-disk-csi-driver** project accepts contribution via GitHub [pull request](https://help.github.com/articles/about-pull-requests/). This document outlines the process to help get your contribution accepted.

## Sign the Contributor License Agreement

We'd love to accept your patches! Before you start working with cloud-director-named-disk-csi-driver, please read our [Developer Certificate of Origin](https://cla.vmware.com/dco). All contributions to this repository must be signed as described on that page. Your signature certifies that you wrote the patch or have the right to pass it on as an open-source patch.

## Reporting an issue

If you find a bug or a feature request related to cloud-director-named-disk-csi-driver you can create a new GitHub issue in this repo.

## Development Environment

1. Install GoLang 1.16.x and set up your dev environment with `GOPATH`
2. Check out code into `$GOPATH/github.com/vmware/cloud-director-named-disk-csi-driver`

## Building container image for the CSI driver

1. Ensure that you have `docker` installed in your dev/build machine.
2. Run `REGISTRY=<registry where you want to push your images> make csi`

## Contributing a Patch

1. Submit an issue describing your proposed change to the repo.
2. Fork the cloud-director-named-disk-csi-driver repo, develop and test your code changes.
3. Submit a pull request.

## Contact

Please use Github Pull Requests and Github Issues as a means to start a conversation with the team.
