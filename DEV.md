# Development environment

## VSCode

We provide a devcontainer for developing this operator inside vscode. Install the [Microsoft remote containers extension](https://marketplace.visualstudio.com/items?itemName=ms-VSCode-remote.remote-containers) and when you open the workspace a popup will appear prompting you to reopen in the dev container. Choosing this will start a dev container and:
- install relevant extensions (go, kubernetes, shellcheck, gitlens)
- install a KinD k8s cluster 
- configure a launch.json for debugging

The VSCode terminal will be a shell running inside the dev container, so scripts defined in the following section will run fine.
Note the first time you load the dev container can take a while, and there’s no output to show it’s making progress! Be patient - subsequent starts will be much faster.

To test a deployment under the debugger, hit F5 to start the provisioner under the debugger (setting any breakpoints you’d like to trap). Then run `up.sh --nop` to provision the photo gallery app. The `--nop` option prevents `up.sh` from starting its own provisioner.

## Scripts

The [examples](examples/) directory holds sample yaml files that can be used to deploy a demo photo gallery app. You'll need to edit the `storageclass.yaml` in the greeenfield and brownfield subdirectories match your environment - in particular setting the HyperStore S3 and IAM endpoint URLs. You'll also need to edit the `owner-secret.yaml` file to provide your access token and secret key.

The [scripts](scripts/) directory contains a number of scripts to easy starting/tearning down test deployments defined in the examples directory.

- [setkey.sh](scripts/setkey.sh): Copies your credentials from your $HOME/.aws/credentials into examples/owner-secret.yaml
- [up.sh](scripts/up.sh): deploys the photo app using greenfield/brownfield deployment. The provisioner runs outside of k8s. Once successfully deployed, go to http://localhost:30007 to view the app (use for 30008 for brownfield deployment)
- [down.sh](scripts/down.sh): undoes [up.sh](scripts/up.sh)
- [up-prod.sh](scripts/up-prod.sh):  deploys the provisioner inside k8s and then deploys the photo app
- [down-prod.sh](scripts/down-prod.sh): undoes [up-prod.sh](scripts/up-prod.sh)
- [release.sh](scripts/release.sh): builds an alpine based docker image that runs the provisioner. It will also load it into your KinD k8s cluster so you can test a deployment before pushing to quay.io (with release.sh --push).

These scripts and environment have been tested on MacOS and Linux. The VSCode environment also works on Windows with remote containers.
