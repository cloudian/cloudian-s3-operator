module github.com/cloudian/cloudian-s3-operator

go 1.14

require (
	github.com/aws/aws-sdk-go v1.31.5
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/kube-object-storage/lib-bucket-provisioner v0.0.0-20221122204822-d1a8c34382f1
	//k8s.io/api v0.0.0-20190313115550-3c12c96769cc
	k8s.io/api v0.23.5
	k8s.io/apimachinery v0.23.5
	k8s.io/client-go v0.23.5
)
