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
	"flag"
	"fmt"
	"net/url"
	_ "net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	awsuser "github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	"github.com/golang/glog"
	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	libbkt "github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner"
	apibkt "github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner/api"
	bkterr "github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner/api/errors"

	storageV1 "k8s.io/api/storage/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/aws/aws-sdk-go/aws/credentials"
)

const (
	defaultRegion   = "us-west-1"
	httpPort        = 80
	httpsPort       = 443
	httpsScheme     = "https"
	provisionerName = "cloudian-s3.io/bucket"
	regionInsert    = "<REGION>"
	s3Hostname      = "s3-" + regionInsert + ".amazonaws.com"
	s3BucketArn     = "arn:aws:s3:::%s"
	policyArn       = "arn:aws:iam::%s:policy/%s"
	obStateARN      = "ARN"
	obStateUser     = "UserName"
	maxBucketLen    = 58
	genUserLen      = 5
)

var (
	kubeconfig string
	masterURL  string
)

type awsS3Provisioner struct {
	bucketName string
	region     string
	// s3Endpoint is the url used to connect to the s3 server, or nil to use default
	s3Endpoint *url.URL
	// s3session is the aws session for s3 operations
	s3Session *session.Session
	// s3svc is the aws s3 service based on the session
	s3svc *s3.S3
	// iam3session is the aws session for IAM operations
	iamSession *session.Session
	// iam client service
	iamsvc *awsuser.IAM
	//kube client
	clientset *kubernetes.Clientset
	// access keys for aws acct for the bucket *owner*
	bktOwnerAccessId   string
	bktOwnerSecretKey  string
	bktCreateUser      string
	bktStoragePolicyId string
	bktUserName        string
	bktUserAccessId    string
	bktUserSecretKey   string
	bktUserAccountId   string
	bktUserPolicyArn   string
}

func NewAwsS3Provisioner(cfg *restclient.Config, s3Provisioner awsS3Provisioner) (*libbkt.Provisioner, error) {
	const all_namespaces = ""
	return libbkt.NewProvisioner(cfg, provisionerName, s3Provisioner, all_namespaces)
}

// Return the aws default session.
func awsDefaultSession() (*session.Session, error) {

	glog.V(2).Infof("Creating S3 *default* session")
	return session.NewSession(&aws.Config{
		Region: aws.String(defaultRegion),
		//Credentials: credentials.NewStaticCredentials(os.Getenv),
	})
}

// Return the OB struct with minimal fields filled in.
func (p *awsS3Provisioner) rtnObjectBkt(bktName string) *v1alpha1.ObjectBucket {

	var host string
	var port int
	if p.s3Endpoint != nil {
		host = p.s3Endpoint.Host
		if p.s3Endpoint.Scheme == httpsScheme {
			port = httpsPort
		} else {
			port = httpPort
		}
	} else {
		host = strings.Replace(s3Hostname, regionInsert, p.region, 1)
		port = httpsPort
	}

	conn := &v1alpha1.Connection{
		Endpoint: &v1alpha1.Endpoint{
			BucketHost: host,
			BucketPort: port,
			BucketName: bktName,
			Region:     p.region,
			AdditionalConfigData: map[string]string{
				"": "",
			},
		},
		Authentication: &v1alpha1.Authentication{
			AccessKeys: &v1alpha1.AccessKeys{
				AccessKeyID:     p.bktUserAccessId,
				SecretAccessKey: p.bktUserSecretKey,
			},
		},
		AdditionalState: map[string]string{
			obStateARN:  p.bktUserPolicyArn,
			obStateUser: p.bktUserName,
		},
	}

	return &v1alpha1.ObjectBucket{
		Spec: v1alpha1.ObjectBucketSpec{
			Connection: conn,
		},
	}
}

func (p *awsS3Provisioner) createBucket(bktName string) error {

	bucketinput := &s3.CreateBucketInput{
		Bucket: &bktName,
	}

	req, _ := p.s3svc.CreateBucketRequest(bucketinput)
	if p.bktStoragePolicyId != "" {
		req.HTTPRequest.Header.Add("x-gmt-policyid", p.bktStoragePolicyId)
	}
	err := req.Send()
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeBucketAlreadyExists:
				msg := fmt.Sprintf("Bucket %q already exists", bktName)
				glog.Errorf(msg)
				return bkterr.NewBucketExistsError(msg)
			case s3.ErrCodeBucketAlreadyOwnedByYou:
				msg := fmt.Sprintf("Bucket %q already owned by you", bktName)
				glog.Errorf(msg)
				return bkterr.NewBucketExistsError(msg)
			}
		}
		return fmt.Errorf("Bucket %q could not be created: %v", bktName, err)
	}
	glog.Infof("Bucket %s successfully created", bktName)

	//Now at this point, we have a bucket and an owner
	//we should now create the user for the bucket
	return nil
}

// generates an aws.Config from a provision and service endpoint url
func (p *awsS3Provisioner) awsConfig(endpoint *url.URL) *aws.Config {
	cfg := &aws.Config{
		Region:           aws.String(p.region),
		Credentials:      credentials.NewStaticCredentials(p.bktOwnerAccessId, p.bktOwnerSecretKey, ""),
		S3ForcePathStyle: aws.Bool(true),
	}
	if endpoint != nil {
		cfg.Endpoint = aws.String(endpoint.String())
	}
	return cfg
}

// Create an aws session based on the OBC's storage class's secret and region.
// Set in the receiver the session and region used to create the session.
// Note: in error cases it's possible that the set region is different from
//
//	the OBC's storage class's region.
func (p *awsS3Provisioner) awsSessionFromStorageClass(ctx context.Context, sc *storageV1.StorageClass) error {

	// helper func var for error returns
	var errDefault = func() error {
		var err error
		p.region = defaultRegion
		p.s3Session, err = awsDefaultSession()
		p.iamSession = p.s3Session
		return err
	}

	region := getRegion(sc)
	if region == "" {
		glog.Infof("region is empty in storage class %q, default region %q used", sc.Name, defaultRegion)
		region = defaultRegion
	}
	secretNS, secretName := getSecretName(sc)
	if secretNS == "" || secretName == "" {
		glog.Infof("secret name or namespace are empty in storage class %q", sc.Name)
		return errDefault()
	}

	// get the sc's bucket owner secret
	accessKeyId, secretKey, err := credsFromSecret(ctx, p.clientset, secretNS, secretName)
	if err != nil {
		glog.Warningf("secret \"%s/%s\" in storage class %q for %q is empty.\nUsing default credentials.", secretNS, secretName, sc.Name, p.bucketName)
		return errDefault()
	}
	p.bktOwnerAccessId = accessKeyId
	p.bktOwnerSecretKey = secretKey

	// get the s3 and iam endpoints
	s3URL, err := getS3ApiURL(sc)
	if err != nil {
		glog.Errorf("Invalid S3 API URL in storage class %s: %v", sc.Name, err)
		return err
	}
	iamURL, err := getIAMApiURL(sc)
	if err != nil {
		glog.Errorf("Invalid IAM API URL in storage class %s: %v", sc.Name, err)
		return err
	}

	// use the OBC's SC to create our sessions, set receiver fields
	glog.V(2).Infof("Creating S3 session using credentials from storage class %s's secret", sc.Name)
	p.region = region
	p.s3Endpoint = s3URL
	p.s3Session, err = session.NewSession(p.awsConfig(s3URL))
	if err == nil {
		p.iamSession, err = session.NewSession(p.awsConfig(iamURL))
	}

	return err
}

// Create the AWS session and S3 service and store them to the receiver.
func (p *awsS3Provisioner) setSessionAndService(ctx context.Context, sc *storageV1.StorageClass) error {
	// set the aws session
	glog.V(2).Infof("Creating S3 session based on storageclass %q", sc.Name)
	err := p.awsSessionFromStorageClass(ctx, sc)
	if err != nil {
		return fmt.Errorf("error creating AWS session: %v", err)
	}

	glog.V(2).Infof("Creating S3 service based on storageclass %q", sc.Name)
	p.s3svc = s3.New(p.s3Session)
	if p.s3svc == nil {
		return fmt.Errorf("error creating S3 service: %v", err)
	}

	return nil
}

// initializeCreateOrGrant sets common provisioner receiver fields and
// the services and sessions needed to provision.
func (p *awsS3Provisioner) initializeCreateOrGrant(ctx context.Context, options *apibkt.BucketOptions) error {
	glog.V(2).Infof("initializing and setting CreateOrGrant services")
	// set the bucket name
	p.bucketName = options.BucketName

	// get the OBC and its storage class
	obc := options.ObjectBucketClaim
	scName := options.ObjectBucketClaim.Spec.StorageClassName
	sc, err := p.getClassByNameForBucket(ctx, scName)
	if err != nil {
		glog.Errorf("failed to get storage class for OBC \"%s/%s\": %v", obc.Namespace, obc.Name, err)
		return err
	}

	// check for bkt user access policy vs. bkt owner policy based on SC
	p.setCreateBucketUserOptions(sc)

	// check if storage policy is defined
	const scPolicy = "storagePolicyId"
	if policy, ok := sc.Parameters[scPolicy]; ok {
		p.bktStoragePolicyId = policy
	}

	// set the aws session and s3 service from the storage class
	err = p.setSessionAndService(ctx, sc)
	if err != nil {
		return fmt.Errorf("error using OBC \"%s/%s\": %v", obc.Namespace, obc.Name, err)
	}

	return nil
}

// initializeUserAndPolicy sets commonly used provisioner
// receiver fields, generates a unique username and calls
// handleUserandPolicy.
func (p *awsS3Provisioner) initializeUserAndPolicy(ctx context.Context, options *apibkt.BucketOptions) error {

	scName := options.ObjectBucketClaim.Spec.StorageClassName
	var err error
	var uAccess, uKey string

	if p.bktCreateUser == "yes" {
		//Create IAM service (maybe this should be added into our default or obc session
		//or create all services type of function?
		p.iamsvc = awsuser.New(p.iamSession)

		// Create a new IAM user using the name of the bucket and set
		// access and attach policy for bucket and user
		p.bktUserName = p.createUserName(p.bucketName)

		// handle all iam and policy operations
		uAccess, uKey, err = p.handleUserAndPolicy(p.bucketName, options)
	} else if uSecretName, ok := options.Parameters["bucketClaimUserSecretName"]; ok {
		// Extract the bucket user secret
		uSecretNS := options.Parameters["bucketClaimUserSecretNamespace"]
		// get the sc's bucket owner secret
		uAccess, uKey, err = credsFromSecret(ctx, p.clientset, uSecretNS, uSecretName)
		if err != nil {
			glog.Errorf("secret \"%s/%s\" in storage class %s for %q is invalid: %v", uSecretNS, uSecretName, scName, p.bucketName, err)
		}
	} else {
		// Default to using the bucket owner creds
		uAccess = p.bktOwnerAccessId
		uKey = p.bktOwnerSecretKey
	}
	if err == nil {
		p.bktUserAccessId = uAccess
		p.bktUserSecretKey = uKey
	}
	return err
}

func (p *awsS3Provisioner) checkIfBucketExists(name string) bool {

	input := &s3.HeadBucketInput{
		Bucket: aws.String(name),
	}

	_, err := p.s3svc.HeadBucket(input)
	if err != nil {
		if err.(awserr.Error).Code() == s3.ErrCodeNoSuchBucket {
			return false
		}
		return false
	}

	return true
}

func (p *awsS3Provisioner) checkIfUserExists(name string) bool {

	input := &awsuser.GetUserInput{
		UserName: aws.String(name),
	}

	_, err := p.iamsvc.GetUser(input)
	if err != nil {
		return err.(awserr.Error).Code() == awsuser.ErrCodeEntityAlreadyExistsException
	}

	return false
}

// GenerateUserID should deterministically generate a user ID for an OBC. This ID is used as an
// idempotency key in order to ensure repeat calls to Provision or Grant are consistent.
// Lib-bucket-provisioner may pass a non-nil ObjectBucket with the function as well. This will
// be nil if the ObjectBucket does not yet exist, but it will be non-nil for existing buckets.
// This may help provisioners recover the idempotency key from a pre-existing bucket.
func (p awsS3Provisioner) GenerateUserID(obc *v1alpha1.ObjectBucketClaim, ob *v1alpha1.ObjectBucket) (string, error) {
	return obc.Name, nil
}

// Provision creates an aws s3 bucket and returns a connection info
// representing the bucket's endpoint and user access credentials.
// Programming Note: _all_ methods on "awsS3Provisioner" called directly
//
//	or indirectly by `Provision` should use pointer receivers. This allows
//	all supporting methods to set receiver fields where convenient. An
//	alternative (arguably better) would be for all supporting methods
//	to take value receivers and functionally return back to `Provision`
//	receiver fields they need set. The first approach is easier for now.
func (p awsS3Provisioner) Provision(options *apibkt.BucketOptions) (*v1alpha1.ObjectBucket, error) {
	ctx := context.Background()

	// initialize and set the AWS services and commonly used variables
	err := p.initializeCreateOrGrant(ctx, options)
	if err != nil {
		return nil, err
	}

	// create the bucket
	glog.Infof("Creating bucket %q", p.bucketName)
	err = p.createBucket(p.bucketName)
	if err != nil {
		err = fmt.Errorf("error creating bucket %q: %v", p.bucketName, err)
		glog.Errorf(err.Error())
		return nil, err
	}
	// If we have a failure in the remainder of the provisioning, delete this bucket
	defer func() {
		if err != nil {
			_, delerr := p.s3svc.DeleteBucket(&s3.DeleteBucketInput{
				Bucket: aws.String(p.bucketName),
			})
			if delerr != nil {
				glog.Errorf("Error undoing bucket creation %s: %v", p.bucketName, delerr)
			}
		}
	}()

	// createBucket was successful, deal with user and policy
	// Bucket does exist, attach new user and policy wrapper
	// calling initializeCreateOrGrant
	// TODO: we currently are catching an error that is always nil
	err = p.initializeUserAndPolicy(ctx, options)
	if err != nil {
		err = fmt.Errorf("error creating user for bucket %q: %v", p.bucketName, err)
		glog.Errorf(err.Error())
		return nil, err
	}

	// returned ob with connection info
	return p.rtnObjectBkt(p.bucketName), nil
}

// Grant attaches to an existing aws s3 bucket and returns a connection info
// representing the bucket's endpoint and user access credentials.
func (p awsS3Provisioner) Grant(options *apibkt.BucketOptions) (*v1alpha1.ObjectBucket, error) {
	ctx := context.Background()

	// initialize and set the AWS services and commonly used variables
	err := p.initializeCreateOrGrant(ctx, options)
	if err != nil {
		return nil, err
	}

	// check and make sure the bucket exists
	glog.Infof("Checking for existing bucket %q", p.bucketName)
	if !p.checkIfBucketExists(p.bucketName) {
		return nil, fmt.Errorf("bucket %s does not exist", p.bucketName)
	}

	// Bucket does exist, attach new user and policy wrapper
	// calling initializeUserAndPolicy
	// TODO: we currently are catching an error that is always nil
	err = p.initializeUserAndPolicy(ctx, options)
	if err != nil {
		err = fmt.Errorf("error creating user for bucket %q: %v", p.bucketName, err)
		glog.Errorf(err.Error())
		return nil, err
	}

	// returned ob with connection info
	// TODO: assuming this is the same Green vs Brown?
	return p.rtnObjectBkt(p.bucketName), nil
}

// Delete the bucket and all its objects.
// Note: only called when the bucket's reclaim policy is "delete".
func (p awsS3Provisioner) Delete(ob *v1alpha1.ObjectBucket) error {
	ctx := context.Background()

	// set receiver fields from OB data
	p.bucketName = ob.Spec.Endpoint.BucketName
	p.bktUserPolicyArn = ob.Spec.AdditionalState[obStateARN]
	p.bktUserName = ob.Spec.AdditionalState[obStateUser]
	scName := ob.Spec.StorageClassName
	glog.Infof("Deleting bucket %q for OB %q", p.bucketName, ob.Name)

	// get the OB and its storage class
	sc, err := p.getClassByNameForBucket(ctx, scName)
	if err != nil {
		return fmt.Errorf("failed to get storage class for OB %q: %v", ob.Name, err)
	}

	// set the aws session and s3 service from the storage class
	err = p.setSessionAndService(ctx, sc)
	if err != nil {
		return fmt.Errorf("error using OB %q: %v", ob.Name, err)
	}

	// Delete IAM Policy and User
	p.setCreateBucketUserOptions(sc)
	err = p.handleUserAndPolicyDeletion(p.bucketName)
	if err != nil {
		glog.Errorf("Failed to delete Policy and/or User - manual clean up required")
		return fmt.Errorf("Error deleting Policy and/or User %v", err)
	}

	// Delete Bucket
	iter := s3manager.NewDeleteListIterator(p.s3svc, &s3.ListObjectsInput{
		Bucket: aws.String(p.bucketName),
	})

	glog.V(2).Infof("Deleting all objects in bucket %q (from OB %q)", p.bucketName, ob.Name)
	err = s3manager.NewBatchDeleteWithClient(p.s3svc).Delete(aws.BackgroundContext(), iter)
	if err != nil && !isNoSuchBucketError(err) {
		return fmt.Errorf("Error deleting objects from bucket %q: %v", p.bucketName, err)
	}

	glog.V(2).Infof("Deleting empty bucket %q from OB %q", p.bucketName, ob.Name)
	_, err = p.s3svc.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(p.bucketName),
	})
	if err != nil && !isNoSuchBucketError(err) {
		return fmt.Errorf("Error deleting empty bucket %q: %v", p.bucketName, err)
	}
	glog.Infof("Deleted bucket %q from OB %q", p.bucketName, ob.Name)

	return nil
}

// Revoke removes a user, policy and access keys from an existing bucket.
func (p awsS3Provisioner) Revoke(ob *v1alpha1.ObjectBucket) error {
	ctx := context.Background()

	// set receiver fields from OB data
	p.bucketName = ob.Spec.Endpoint.BucketName
	p.bktUserPolicyArn = ob.Spec.AdditionalState[obStateARN]
	p.bktUserName = ob.Spec.AdditionalState[obStateUser]
	scName := ob.Spec.StorageClassName
	glog.Infof("Revoking access to bucket %q for OB %q", p.bucketName, ob.Name)

	// get the OB and its storage class
	sc, err := p.getClassByNameForBucket(ctx, scName)
	if err != nil {
		return fmt.Errorf("failed to get storage class for OB %q: %v", ob.Name, err)
	}

	// set the aws session and s3 service from the storage class
	err = p.setSessionAndService(ctx, sc)
	if err != nil {
		return fmt.Errorf("error using OB %q: %v", ob.Name, err)
	}

	// Delete IAM Policy and User
	p.setCreateBucketUserOptions(sc)
	err = p.handleUserAndPolicyDeletion(p.bucketName)
	if err != nil {
		// We are currently only logging
		// because if failure do not want to stop
		// deletion of bucket
		glog.Errorf("Failed to delete Policy and/or User - manual clean up required")
		//return fmt.Errorf("Error deleting Policy and/or User %v", err)
	}

	// No Deletion of Bucket or Content in Brownfield
	return nil
}

// create k8s config and client for the runtime-controller.
// Note: panics on errors.
func createConfigAndClientOrDie(masterurl, kubeconfig string) (*restclient.Config, *kubernetes.Clientset) {
	config, err := clientcmd.BuildConfigFromFlags(masterurl, kubeconfig)
	if err != nil {
		glog.Fatalf("Failed to create config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create client: %v", err)
	}
	return config, clientset
}

func main() {
	defer glog.Flush()
	syscall.Umask(0)

	handleFlags()

	glog.Infof("Cloudian S3 Operator - main")
	glog.V(2).Infof("flags: kubeconfig=%q; masterURL=%q", kubeconfig, masterURL)

	config, clientset := createConfigAndClientOrDie(masterURL, kubeconfig)

	stopCh := handleSignals()

	s3Prov := awsS3Provisioner{}
	s3Prov.clientset = clientset

	// Create and run the s3 provisioner controller.
	// It implements the Provisioner interface expected by the bucket
	// provisioning lib.
	S3ProvisionerController, err := NewAwsS3Provisioner(config, s3Prov)
	if err != nil {
		glog.Errorf("killing Cloudian S3 operator, error initializing library controller: %v", err)
		os.Exit(1)
	}
	glog.V(2).Infof("main: running %s provisioner...", provisionerName)
	err = S3ProvisionerController.Run(stopCh)
	if err != nil {
		glog.Errorf("Cloudian S3 operator, error running operator : %v", err)
	}

	<-stopCh
	glog.Infof("main: %s operator exited.", provisionerName)
}

// Set -kubeconfig and (deprecated) -master flags.
// Note: when the bucket library used the controller-runtime, -kubeconfig and -master were
//
//	set its config package's init() function. Now this is done here.
func handleFlags() {

	flag.StringVar(&kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", os.Getenv("MASTER"), "(Deprecated: use `--kubeconfig`) The address of the Kubernetes API server. Overrides kubeconfig. Only required if out-of-cluster.")

	if !flag.Parsed() {
		flag.Parse()
	}
}

// Shutdown gracefully on system signals.
func handleSignals() <-chan struct{} {
	sigCh := make(chan os.Signal)
	stopCh := make(chan struct{})
	go func() {
		signal.Notify(sigCh, syscall.SIGTERM)
		<-sigCh
		close(stopCh)
		os.Exit(1)
	}()
	return stopCh
}
