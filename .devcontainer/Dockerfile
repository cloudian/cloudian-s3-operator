FROM ubuntu:22.04

ARG USERNAME=builder
ARG GO_VERSION=1.20.5
ARG KUBECTL_VERSION=v1.25.3
ARG KIND_VERSION=v0.17.0
ARG K3D_VERSION=v5.5.1

# Configure apt, install packages and tools
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
    && apt-get -y install --no-install-recommends apt-utils dialog 2>&1 \
    && apt-get -y install bash-completion curl gcc git openssh-client less iproute2 procps lsb-release docker.io docker-compose sudo \
    && apt-get autoremove -y \
    && apt-get clean -y \
    && rm -rf /var/lib/apt/lists/*

# Install go
RUN (curl -sSfL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz | tar xz --directory=/usr/local)
ENV PATH=$PATH:/usr/local/go/bin

# Install kubectl
RUN curl -sSfLo /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl \
    && chmod +x /usr/local/bin/kubectl \
    && kubectl completion bash > /etc/bash_completion.d/kubectl

# Install kubernetes in docker
RUN curl -sSfLo /usr/local/bin/kind https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-$(uname)-amd64 \
    && chmod +x /usr/local/bin/kind \
    && kind completion bash > /etc/bash_completion.d/kind

# Install k3d
RUN curl -sSfLo /usr/local/bin/k3d https://github.com/k3d-io/k3d/releases/download/${K3D_VERSION}/k3d-linux-amd64 \
    && chmod +x /usr/local/bin/k3d \
    && k3d completion bash > /etc/bash_completion.d/k3d

# Create a non-root user to use 
RUN groupadd $USERNAME \
    && useradd -s /bin/bash --gid $USERNAME --create-home $USERNAME --groups docker \
    # Add sudo support
    && echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME \
    && chmod 0440 /etc/sudoers.d/$USERNAME \
    # Add users go bin to path
    && echo 'PATH=$PATH:$HOME/go/bin' >> /home/$USERNAME/.bashrc \
    # Create bind mount points and ensure they're owned by $USERNAME
    && bash -c 'mkdir -p /home/$USERNAME/{.aws,.docker,.kube} && chown $USERNAME:$USERNAME /home/$USERNAME/{.aws,.docker,.kube}'

COPY --chown=builder:builder inputrc /home/builder/.inputrc

COPY entrypoint.sh /entrypoint.sh
ENTRYPOINT [ "/entrypoint.sh" ]
