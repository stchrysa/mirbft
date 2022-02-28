#!/bin/bash

. vars.sh

echo "Installing Ubuntu packages."

sudo add-apt-repository -y ppa:longsleep/golang-backports

sudo apt-get -y update
sudo apt-get -y install \
	protobuf-compiler \
	protobuf-compiler-grpc \
	git \
	openssl \
	jq \
	graphviz\
	golang-go

cd ~

echo "Setting up stubborn scp."
mkdir -p "/root/bin"
echo '#!/bin/bash

retry_limit=$1
shift

scp "$@"
exit_status=$?
while [ $exit_status -ne 0 ] && [ $retry_limit -gt 0 ]; do
  >&2 echo "scp failed. Retrying. Attempts left: $retry_limit"
  retry_limit=$((retry_limit - 1))
  sleep 2
  scp "$@"
  exit_status=$?
done
exit $exit_status
' > "/root/bin/stubborn-scp.sh"
chmod u+x "/root/bin/stubborn-scp.sh"

cd ~

mkdir -p go

echo "Configuring golang."

export GOPATH=/root/go
export PATH=$PATH:$GOPATH/bin
export PATH=$PATH:/root/bin
export GOCACHE=~/.cache/go-build
export GIT_SSL_NO_VERIFY=1
export GO111MODULE=off

cat << EOF >> ~/.bashrc
export GOPATH=/root/go
export PATH=$PATH:$GOPATH/bin
export PATH=$PATH:/root/bin
export GOCACHE=~/.cache/go-build
export GIT_SSL_NO_VERIFY=1
export GO111MODULE=off
EOF

echo "Installing golang packages. (May take a long time without producing output.)"

echo "Installing gRPC for Go."
go get -u google.golang.org/grpc

echo "Installing Protobufs for Go."
go get -u github.com/golang/protobuf/protoc-gen-go

echo "Installing Zerolog for Go."
go get -u github.com/rs/zerolog/log

echo "Installing Linux Goprocinfo for Go"
go get -u github.com/c9s/goprocinfo/linux

echo "Installing Kyber for Go"
go get -u go.dedis.ch/kyber
go get go.dedis.ch/fixbuf
go get golang.org/x/crypto/blake2b

echo "Installing the YAML parser for Go"
go get -u gopkg.in/yaml.v2
