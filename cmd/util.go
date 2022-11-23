/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/golang/glog"
	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	storageV1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Return the storage class for a given name.
func (p *awsS3Provisioner) getClassByNameForBucket(ctx context.Context, className string) (*storageV1.StorageClass, error) {

	glog.V(2).Infof("getting storage class %q...", className)
	class, err := p.clientset.StorageV1().StorageClasses().Get(ctx, className, metav1.GetOptions{})
	// TODO: retry w/ exponential backoff
	if err != nil {
		return nil, fmt.Errorf("unable to Get storageclass %q: %v", className, err)
	}
	return class, nil
}

// Return the region name from the passed in storage class.
func getRegion(sc *storageV1.StorageClass) string {

	const scRegionKey = "region"
	return sc.Parameters[scRegionKey]
}

// Return the secret namespace and name from the passed storage class.
func getSecretName(sc *storageV1.StorageClass) (string, string) {

	const (
		scSecretNameKey = "secretName"
		scSecretNSKey   = "secretNamespace"
	)
	return sc.Parameters[scSecretNSKey], sc.Parameters[scSecretNameKey]
}

// getApiURL returns the URL configured in a storage class for a given api, or nil if not present
func getApiURL(sc *storageV1.StorageClass, apikey string) (u *url.URL, err error) {
	if v, ok := sc.Parameters[apikey]; ok {
		// Check it's a valid uri
		if u, err = url.Parse(v); err != nil {
			return
		}
		// Support just having an endpoint name, assume https if that's the case
		if u.Scheme == "" {
			if u, err = url.Parse("https://" + v); err != nil {
				return
			}
		}
		// We don't expect the URL to have a path
		if u.Path != "" {
			err = fmt.Errorf("Invalid API endpoint: key=%s, value=%s", apikey, v)
		}
	}
	return
}

// getS3ApiUri returns the s3 uri configured in a storage class, or "" if not present
func getS3ApiURL(sc *storageV1.StorageClass) (*url.URL, error) {
	const scS3Endpoint = "s3Endpoint"
	return getApiURL(sc, scS3Endpoint)
}

// getIAMApiUri returns the s3 uri configured in a storage class, or "" if not present
func getIAMApiURL(sc *storageV1.StorageClass) (*url.URL, error) {
	const scIAMEndpoint = "iamEndpoint"
	return getApiURL(sc, scIAMEndpoint)
}

// Get the secret and set the receiver to the accessKeyId and secretKey.
func credsFromSecret(ctx context.Context, c *kubernetes.Clientset, ns, name string) (accessKeyId, secretKey string, err error) {

	nsName := fmt.Sprintf("%s/%s", ns, name)
	glog.V(2).Infof("getting secret %q...", nsName)
	secret, err := c.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		// TODO: some kind of exponential backoff and retry...
		return
	}

	accessKeyId = string(secret.Data[v1alpha1.AwsKeyField])
	secretKey = string(secret.Data[v1alpha1.AwsSecretField])
	if accessKeyId == "" || secretKey == "" {
		err = fmt.Errorf("accessId and/or secretKey are blank in secret \"%s/%s\"", secret.Namespace, secret.Name)
	}

	return
}

func randomString(n int) string {

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	var letterRunes = []rune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[r.Intn(len(letterRunes))]
	}
	return string(b)
}

func (p *awsS3Provisioner) createUserName(bkt string) string {
	// prefix is bucket name
	if len(bkt) > maxBucketLen {
		bkt = bkt[:(maxBucketLen - 1)]
	}

	var userbool bool
	name := ""
	i := 0
	for ok := true; ok; ok = userbool {
		name = fmt.Sprintf("%s-%s", bkt, randomString(genUserLen))
		userbool = p.checkIfUserExists(name)
		i++
	}
	glog.V(2).Infof("Generated user %s after %v iterations", name, i)
	return name
}

// isNoSuchBucketError tests the result of failed bucket deletion calls
// to see if the failure was due to the bucket already having been deleted.
// This can happen if the provisioner restarts during a Delete() operation.
func isNoSuchBucketError(err error) bool {
	// Handle NewBatchDeleteWithClient errors
	if batchError, ok := err.(*s3manager.BatchError); ok {
		if origErr, ok := batchError.Errors[0].OrigErr.(awserr.Error); ok {
			code := origErr.Code()
			return code == s3.ErrCodeNoSuchBucket
		}
	}
	// Handle DeleteBucket errors
	if origErr, ok := err.(awserr.Error); ok {
		code := origErr.Code()
		return code == s3.ErrCodeNoSuchBucket
	}
	return false
}

// isNoSuchBucketError tests the result of an IAM delete operation
// to see if the failure was due to the entity already having been removed
func isNoSuchEntityError(err error) bool {
	if awsErr, ok := err.(awserr.Error); ok {
		return awsErr.Code() == iam.ErrCodeNoSuchEntityException
	}
	return false
}
