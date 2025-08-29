# EKS Setup for `urunc`
In this tutorial, we’ll walk through the complete process of provisioning an Amazon EKS (Elastic Kubernetes Service) cluster from scratch using the AWS CLI, `eksctl`, and a few supporting tools.

Our goal is to create a Kubernetes-native environment capable of securely running containers with [`urunc`](https://github.com/urunc-dev/urunc) — a unikernel container runtime. This tutorial sets up a production-grade EKS cluster, complete with custom networking, Calico CNI plugin for fine-grained pod networking, and node groups ready to schedule unikernel workloads.

We’ll cover:

-  Tooling prerequisites
-  VPC and networking setup
-  Cluster bootstrapping with `eksctl`
-  Calico installation and configuration
-  Managed node group provisioning
-  `urunc` installation
-  Example deployment of unikernels

## Tooling Setup for EKS Cluster Provisioning
This section ensures your local environment is equipped with all the required tools to interact with AWS and provision your EKS cluster.

### Prerequisites
You'll need the following CLI tools installed and configured:

#### 1. AWS CLI
Used to interact with AWS services like IAM, EC2, CloudFormation, etc.

Install AWS CLI (v2 recommended)
```bash
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
sudo ./aws/install
```

Verify installation:
```bash
aws --version
```

Configure it with your credentials:
```bash
aws configure
```

You'll be prompted to enter:

-   AWS Access Key ID 
-   AWS Secret Access Key
-   Default region (e.g., `eu-central-1`)
-   Default output format (e.g., `json`)

#### 2. eksctl
The official CLI tool for managing EKS clusters.

Download and install eksctl:

```bash
curl --silent --location "https://github.com/weaveworks/eksctl/releases/latest/download/eksctl_$(uname -s)_amd64.tar.gz" | tar xz -C /tmp
sudo mv /tmp/eksctl /usr/local/bin
```

Verify the installation:
```bash
eksctl version
```

#### 3. kubectl

The Kubernetes CLI used to interact with your EKS cluster.

Install kubectl (replace version as needed):

```bash
curl -LO "https://dl.k8s.io/release/v[[ versions.kubectl ]]/bin/linux/amd64/kubectl"
chmod +x kubectl
sudo mv kubectl /usr/local/bin/
```

Verify the installation:
```bash
kubectl version --client
```

#### 4. jq

A lightweight and flexible command-line JSON processor, used in helper scripts.

```bash
sudo apt-get update
sudo apt-get install -y jq
```

#### 5. SSH Keypair (for Node Access)

Ensure you have a key pair uploaded to AWS for SSH access to EC2 instances.

Generate an SSH key if you don’t have one:
```bash
ssh-keygen -t rsa -b 4096 -f ~/.ssh/awseks -N ""
```
Import the public key into AWS (or use an existing one)

```bash
aws ec2 import-key-pair \
  --key-name awseks \
  --public-key-material fileb://~/.ssh/awseks.pub
```

## Cluster Setup

We begin by provisioning an Amazon EKS cluster with private subnets and Calico as the CNI instead of the default AWS CNI.

### VPC with Private Subnets

We use the official EKS CloudFormation template to create a VPC with private subnets.

```bash
export STACK_NAME="urunc-tutorial"
export REGION="eu-central-1"
aws cloudformation create-stack \
  --region $REGION \
  --stack-name $STACK_NAME \
  --template-url https://s3.us-west-2.amazonaws.com/amazon-eks/cloudformation/2020-10-29/amazon-eks-vpc-private-subnets.yaml
```

The output of the above command would verify the successful creation of the VPC:

```json
{
    "StackId": "arn:aws:cloudformation:eu-central-1:058264306458:stack/urunc-tutorial/ec8ae800-0fbc-11f0-bda2-0a29df3fde61"
}
```

### Create IAM Role for the EKS Cluster

We define a trust policy allowing EKS to assume a role. Create a json file
(e.g. `eks-cluster-role-trust-policy.json`) with the following contents:
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "eks.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
```

Create the role:

```bash
aws iam create-role \
  --role-name uruncTutorialRole \
  --assume-role-policy-document file://eks-cluster-role-trust-policy.json
```

Attach the required EKS policy:
```bash
aws iam attach-role-policy \
  --policy-arn arn:aws:iam::aws:policy/AmazonEKSClusterPolicy \
  --role-name uruncTutorialRole
```

### Extract Public Subnet IDs (if needed)

This helper script (`get_pub_subnets.sh`) identifies public subnets in the current region by checking for routes to an Internet Gateway:

```bash
#!/bin/bash

REGION="eu-central-1"
subnets=$(aws ec2 describe-subnets --query 'Subnets[*].{ID:SubnetId}' --output text --region $REGION)
route_tables=$(aws ec2 describe-route-tables --query 'RouteTables[*].{ID:RouteTableId,Associations:Associations[*].SubnetId,Routes:Routes[*]}' --output json --region $REGION)

public_subnets=()

for subnet in $subnets; do
  associated_route_table=$(echo $route_tables | jq -r --arg SUBNET "$subnet" '.[] | select(.Associations[]? == $SUBNET) | .ID')
  if [ -n "$associated_route_table" ]; then
    has_igw=$(echo $route_tables | jq -r --arg RTID "$associated_route_table" '.[] | select(.ID == $RTID) | .Routes[] | select(.GatewayId != null) | .GatewayId' | grep 'igw-')
    if [ -n "$has_igw" ]; then
      public_subnets+=("$subnet")
    fi
  fi
done

public_subnets_str=$(IFS=,; echo "${public_subnets[*]}")
echo "$public_subnets_str"
```

Run it to retrieve subnet IDs:
```bash
bash get_pub_subnets.sh
```

Example output:
```
subnet-02bcaca5ac39eca7a,subnet-0d0667e2156169998
```
#### Create the EKS Cluster with Calico CNI

It is time to set up the cluster and managed node groups with Calico networking.

##### Step 1: Create EKS control plane with private subnets and no initial node group

Use the subnets from the command above.

```bash
export CLUSTER_NAME="urunc-tutorial"
export REGION="eu-central-1"
export SUBNETS="subnet-02bcaca5ac39eca7a,subnet-0d0667e2156169998"
eksctl create cluster \
  --name ${CLUSTER_NAME} \
  --region $REGION \
  --version [[ versions.kubernetes ]] \
  --vpc-private-subnets $SUBNETS \
  --without-nodegroup
```

Example output:
```
2 sequential tasks: { create cluster control plane "urunc-tutorial", wait for control plane to become ready
}
2025-04-02 12:29:16 [ℹ]  building cluster stack "eksctl-urunc-tutorial-cluster"
2025-04-02 12:29:19 [ℹ]  deploying stack "eksctl-urunc-tutorial-cluster"
2025-04-02 12:29:49 [ℹ]  waiting for CloudFormation stack "eksctl-urunc-tutorial-cluster"
[...]
2025-04-02 12:39:26 [ℹ]  waiting for the control plane to become ready
2025-04-02 12:39:27 [✔]  saved kubeconfig as "~/.kube/config"
2025-04-02 12:39:27 [ℹ]  no tasks
2025-04-02 12:39:27 [✔]  all EKS cluster resources for "urunc-tutorial" have been created
2025-04-02 12:39:27 [✔]  created 0 nodegroup(s) in cluster "urunc-tutorial"
2025-04-02 12:39:27 [✔]  created 0 managed nodegroup(s) in cluster "urunc-tutorial"
2025-04-02 12:39:35 [ℹ]  kubectl command should work with "~/.kube/config", try 'kubectl get nodes'
2025-04-02 12:39:35 [✔]  EKS cluster "urunc-tutorial" in "eu-central-1" region is ready
```

Now, you should have the control-plane deployed and ready. The first thing to do is to remove the AWS CNI, as gateway ARP entries are [statically populated](https://github.com/aws/amazon-vpc-cni-k8s/blob/dce8a9c47de31fd682e35e7a0a698a1b9b2eb2f2/cmd/routed-eni-cni-plugin/driver/driver.go#L202).

##### Step 2: Remove AWS CNI

```bash
kubectl delete daemonset -n kube-system aws-node
```

Expected output:
```
daemonset.apps "aws-node" deleted
```

##### Step 3: Add Calico CNI:

```bash
kubectl create -f https://raw.githubusercontent.com/projectcalico/calico/v[[ versions.calico ]]/manifests/tigera-operator.yaml
```

> Note: There are cases where a large set of manifests can cause a failure to
> the above command. If it does, try to re-issue the command.

Expected output:
```
namespace/tigera-operator created
customresourcedefinition.apiextensions.k8s.io/bgpconfigurations.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/bgpfilters.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/bgppeers.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/blockaffinities.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/caliconodestatuses.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/clusterinformations.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/felixconfigurations.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/globalnetworkpolicies.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/globalnetworksets.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/hostendpoints.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/ipamblocks.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/ipamconfigs.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/ipamhandles.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/ippools.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/ipreservations.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/kubecontrollersconfigurations.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/networkpolicies.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/networksets.crd.projectcalico.org created
customresourcedefinition.apiextensions.k8s.io/apiservers.operator.tigera.io created
customresourcedefinition.apiextensions.k8s.io/imagesets.operator.tigera.io created
customresourcedefinition.apiextensions.k8s.io/installations.operator.tigera.io created
customresourcedefinition.apiextensions.k8s.io/tigerastatuses.operator.tigera.io created
serviceaccount/tigera-operator created
clusterrole.rbac.authorization.k8s.io/tigera-operator created
clusterrolebinding.rbac.authorization.k8s.io/tigera-operator created
deployment.apps/tigera-operator created
```

Create an installation resource to provision the `calico-node` daemonset:
```bash
kubectl create -f - <<EOF
kind: Installation
apiVersion: operator.tigera.io/v1
metadata:
  name: default
spec:
  kubernetesProvider: EKS
  cni:
    type: Calico
  calicoNetwork:
    bgp: Disabled
EOF
```

Expected output:

```
installation.operator.tigera.io/default created
```

##### Step 4: Provision nodes

Now, you are ready to provision nodes for the cluster. Use the following description to create two bare-metal nodes, one for each supported architecture (`amd64` and `arm64`):
> Note: Make sure the `metadata.name` entry corresponds to the name you specified for your cluster above, and that the managedNodeGroups.[].subnets entry correspond to the ones specified above.

```bash
eksctl create nodegroup -f - <<EOF
---
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: urunc-tutorial
  region: eu-central-1

managedNodeGroups:
  - name: a1-metal
    instanceType: a1.metal
    amiFamily: Ubuntu2204
    desiredCapacity: 1
    minSize: 1
    maxSize: 1
    volumeSize: 150
    volumeType: gp3
    volumeEncrypted: true
    privateNetworking: true
    ssh:
      allow: true
      publicKeyName: awseks
    subnets: ["subnet-02bcaca5ac39eca7a","subnet-0d0667e2156169998"]
    iam:
      withAddonPolicies:
        cloudWatch: true
  - name: c5-metal
    instanceType: c5.metal
    amiFamily: Ubuntu2204
    desiredCapacity: 1
    minSize: 1
    maxSize: 1
    volumeSize: 150
    volumeType: gp3
    volumeEncrypted: true
    privateNetworking: true
    ssh:
      allow: true
      publicKeyName: awseks
    subnets: ["subnet-02bcaca5ac39eca7a","subnet-0d0667e2156169998"]
    iam:
      withAddonPolicies:
        cloudWatch: true
EOF
```

Example output:
```
2025-04-02 12:39:44 [ℹ]  will use version 1.30 for new nodegroup(s) based on control plane version
2025-04-02 12:39:46 [!]  "aws-node" was not found
2025-04-02 12:39:48 [ℹ]  nodegroup "a1-metal-cni" will use "ami-0eb5f4a5031f47d7b" [Ubuntu2204/1.30]
2025-04-02 12:39:49 [ℹ]  using EC2 key pair "awseks"
2025-04-02 12:39:49 [ℹ]  nodegroup "c5-metal-cni" will use "ami-0375252546bcbdbfa" [Ubuntu2204/1.30]
2025-04-02 12:39:49 [ℹ]  using EC2 key pair "awseks"
2025-04-02 12:39:50 [ℹ]  2 nodegroups (a1-metal-cni, c5-metal-cni) were included (based on the include/exclude rules)
2025-04-02 12:39:50 [ℹ]  will create a CloudFormation stack for each of 2 managed nodegroups in cluster "urunc-tutorial"
2025-04-02 12:39:50 [ℹ]
2 sequential tasks: { fix cluster compatibility, 1 task: {
2 parallel tasks: { create managed nodegroup "a1-metal", create managed nodegroup "c5-metal"
} }
}
2025-04-02 12:39:50 [ℹ]  checking cluster stack for missing resources
2025-04-02 12:39:50 [ℹ]  cluster stack has all required resources
2025-04-02 12:39:51 [ℹ]  building managed nodegroup stack "eksctl-urunc-tutorial-nodegroup-a1-metal-cni"
2025-04-02 12:39:51 [ℹ]  building managed nodegroup stack "eksctl-urunc-tutorial-nodegroup-c5-metal-cni"
2025-04-02 12:39:51 [ℹ]  deploying stack "eksctl-urunc-tutorial-nodegroup-c5-metal"
2025-04-02 12:39:51 [ℹ]  deploying stack "eksctl-urunc-tutorial-nodegroup-a1-metal"
2025-04-02 12:39:51 [ℹ]  waiting for CloudFormation stack "eksctl-urunc-tutorial-nodegroup-c5-metal"
2025-04-02 12:39:51 [ℹ]  waiting for CloudFormation stack "eksctl-urunc-tutorial-nodegroup-a1-metal"
[...]
2025-04-02 12:44:05 [ℹ]  no tasks
2025-04-02 12:44:05 [✔]  created 0 nodegroup(s) in cluster "urunc-tutorial"
2025-04-02 12:44:06 [ℹ]  nodegroup "a1-metal" has 1 node(s)
2025-04-02 12:44:06 [ℹ]  node "ip-192-168-103-211.eu-central-1.compute.internal" is ready
2025-04-02 12:44:06 [ℹ]  waiting for at least 1 node(s) to become ready in "a1-metal"
2025-04-02 12:44:06 [ℹ]  nodegroup "a1-metal" has 1 node(s)
2025-04-02 12:44:06 [ℹ]  node "ip-192-168-103-211.eu-central-1.compute.internal" is ready
2025-04-02 12:44:06 [ℹ]  nodegroup "c5-metal" has 1 node(s)
2025-04-02 12:44:06 [ℹ]  node "ip-192-168-32-137.eu-central-1.compute.internal" is ready
2025-04-02 12:44:06 [ℹ]  waiting for at least 1 node(s) to become ready in "c5-metal"
2025-04-02 12:44:06 [ℹ]  nodegroup "c5-metal" has 1 node(s)
2025-04-02 12:44:06 [ℹ]  node "ip-192-168-32-137.eu-central-1.compute.internal" is ready
2025-04-02 12:44:06 [✔]  created 2 managed nodegroup(s) in cluster "urunc-tutorial"
2025-04-02 12:44:07 [ℹ]  checking security group configuration for all nodegroups
2025-04-02 12:44:07 [ℹ]  all nodegroups have up-to-date cloudformation templates
```

##### Step 5: Enable SSH access (optional)

Finally, for debug purposes, enable external SSH access to the nodes:
> Note: Example for one of the two security groups
```bash
aws ec2 authorize-security-group-ingress --group-id sg-0d655f9002aec154e --protocol tcp --port 22 --cidr 0.0.0.0/0 --region eu-central-1
```

Example output:
```json
{
    "Return": true,
    "SecurityGroupRules": [
        {
            "SecurityGroupRuleId": "sgr-09634d2d1eb260e7a",
            "GroupId": "sg-0d655f9002aec154e",
            "GroupOwnerId": "058264306458",
            "IsEgress": false,
            "IpProtocol": "tcp",
            "FromPort": 22,
            "ToPort": 22,
            "CidrIpv4": "0.0.0.0/0",
            "SecurityGroupRuleArn": "arn:aws:ec2:eu-central-1:058264306458:security-group-rule/sgr-09634d2d1eb260e7a"
        }
    ]
}
```

Below is a script to enable external SSH access to all security groups:
> Note: Careful, this exposes SSH access to all of your nodes!
> 
```bash
#!/bin/bash
aws ec2 describe-security-groups --region eu-central-1 --query "SecurityGroups[*].GroupId" --output text | tr '\t' '\n' | \
while read sg_id; do
    echo "Enabling SSH access for $sg_id..."
    aws ec2 authorize-security-group-ingress \
        --group-id "$sg_id" \
        --protocol tcp \
        --port 22 \
        --cidr 0.0.0.0/0 \
        --region eu-central-1 2>&1 | grep -v "InvalidPermission.Duplicate"
done
```

#### Verify the cluster is operational

We have successfully setup the cluster. Let's see what we have using a simple `kubectl get pods -o wide -A`:

```
NAMESPACE         NAME                                       READY   STATUS    RESTARTS   AGE     IP                NODE                                               NOMINATED NODE   READINESS GATES
calico-system     calico-kube-controllers-64cf794c44-jnggx   1/1     Running   0          3m52s   172.16.50.196     ip-192-168-32-137.eu-central-1.compute.internal    <none>           <none>
calico-system     calico-node-xcqrj                          1/1     Running   0          3m47s   192.168.32.137    ip-192-168-32-137.eu-central-1.compute.internal    <none>           <none>
calico-system     calico-node-xn6fc                          1/1     Running   0          3m48s   192.168.103.211   ip-192-168-103-211.eu-central-1.compute.internal   <none>           <none>
calico-system     calico-typha-84546c84b6-86wfx              1/1     Running   0          3m52s   192.168.32.137    ip-192-168-32-137.eu-central-1.compute.internal    <none>           <none>
calico-system     csi-node-driver-jzs7g                      2/2     Running   0          3m52s   172.16.139.1      ip-192-168-103-211.eu-central-1.compute.internal   <none>           <none>
calico-system     csi-node-driver-sstkj                      2/2     Running   0          3m52s   172.16.50.193     ip-192-168-32-137.eu-central-1.compute.internal    <none>           <none>
kube-system       coredns-6f6d89bcc9-dkn6z                   1/1     Running   0          10m     172.16.50.195     ip-192-168-32-137.eu-central-1.compute.internal    <none>           <none>
kube-system       coredns-6f6d89bcc9-ld454                   1/1     Running   0          10m     172.16.50.194     ip-192-168-32-137.eu-central-1.compute.internal    <none>           <none>
kube-system       kube-proxy-7mnbs                           1/1     Running   0          4m3s    192.168.103.211   ip-192-168-103-211.eu-central-1.compute.internal   <none>           <none>
kube-system       kube-proxy-nx5wk                           1/1     Running   0          4m4s    192.168.32.137    ip-192-168-32-137.eu-central-1.compute.internal    <none>           <none>
tigera-operator   tigera-operator-76ff79f7fd-z7t7d           1/1     Running   0          7m17s   192.168.32.137    ip-192-168-32-137.eu-central-1.compute.internal    <none>           <none>
```

Also, let's check out the nodes using `kubectl get nodes --show-labels`:

```console
$ kubectl get nodes --show-labels
NAME                                               STATUS   ROLES    AGE   VERSION   LABELS
ip-192-168-103-211.eu-central-1.compute.internal   Ready    <none>   10m   v1.30.6   alpha.eksctl.io/cluster-name=urunc-tutorial,alpha.eksctl.io/instance-id=i-0f1dc1ede23d8e5a7,alpha.eksctl.io/nodegroup-name=a1-metal-cni,beta.kubernetes.io/arch=arm64,beta.kubernetes.io/instance-type=a1.metal,beta.kubernetes.io/os=linux,eks.amazonaws.com/capacityType=ON_DEMAND,eks.amazonaws.com/nodegroup-image=ami-0eb5f4a5031f47d7b,eks.amazonaws.com/nodegroup=a1-metal-cni,eks.amazonaws.com/sourceLaunchTemplateId=lt-0a89f4d0e008cf6f6,eks.amazonaws.com/sourceLaunchTemplateVersion=1,failure-domain.beta.kubernetes.io/region=eu-central-1,failure-domain.beta.kubernetes.io/zone=eu-central-1b,k8s.io/cloud-provider-aws=8c600fe081bc4d4e16d89383ee5c2ac7,kubernetes.io/arch=arm64,kubernetes.io/hostname=ip-192-168-103-211.eu-central-1.compute.internal,kubernetes.io/os=linux,node-lifecycle=on-demand,node.kubernetes.io/instance-type=a1.metal,topology.k8s.aws/zone-id=euc1-az3,topology.kubernetes.io/region=eu-central-1,topology.kubernetes.io/zone=eu-central-1b
ip-192-168-32-137.eu-central-1.compute.internal    Ready    <none>   10m   v1.30.6   alpha.eksctl.io/cluster-name=urunc-tutorial,alpha.eksctl.io/instance-id=i-033fcef7c9cf7b5aa,alpha.eksctl.io/nodegroup-name=c5-metal-cni,beta.kubernetes.io/arch=amd64,beta.kubernetes.io/instance-type=c5.metal,beta.kubernetes.io/os=linux,eks.amazonaws.com/capacityType=ON_DEMAND,eks.amazonaws.com/nodegroup-image=ami-0375252546bcbdbfa,eks.amazonaws.com/nodegroup=c5-metal-cni,eks.amazonaws.com/sourceLaunchTemplateId=lt-0894d82a5833f577b,eks.amazonaws.com/sourceLaunchTemplateVersion=1,failure-domain.beta.kubernetes.io/region=eu-central-1,failure-domain.beta.kubernetes.io/zone=eu-central-1a,k8s.io/cloud-provider-aws=8c600fe081bc4d4e16d89383ee5c2ac7,kubernetes.io/arch=amd64,kubernetes.io/hostname=ip-192-168-32-137.eu-central-1.compute.internal,kubernetes.io/os=linux,node-lifecycle=on-demand,node.kubernetes.io/instance-type=c5.metal,topology.k8s.aws/zone-id=euc1-az2,topology.kubernetes.io/region=eu-central-1,topology.kubernetes.io/zone=eu-central-1a
```
Let's do a test deployment. Create a file called `nginx-test-deployment.yaml` with the following content:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-stock
  labels:
    app: nginx-stock
spec:
  replicas: 2
  selector:
    matchLabels:
      app: nginx-stock
  template:
    metadata:
      labels:
        app: nginx-stock
    spec:
      containers:
      - name: nginx-stock
        image: nginx
        imagePullPolicy: IfNotPresent
        resources:
          #limits:
            #memory: 768Mi
          requests:
            memory: 60Mi
```

And deploy it:
```bash
kubectl apply -f nginx-test-deployment.yaml
```

This should deploy 2 replicas of NGINX. Check the status:

```bash
kubectl get pods -o wide
```

Example output:
```
NAME                           READY   STATUS    RESTARTS   AGE     IP                NODE                                               NOMINATED NODE   READINESS GATES
nginx-stock-7d54d66484-k9rj5   1/1     Running   0          42s     172.16.50.197     ip-192-168-32-137.eu-central-1.compute.internal    <none>           <none>
nginx-stock-7d54d66484-nn696   1/1     Running   0          42s     172.16.139.2      ip-192-168-103-211.eu-central-1.compute.internal   <none>           <none>
```

And let's try to check network connectivity between pods. Let's run a simple network debug container as a pod:

```bash
kubectl run tmp-shell --rm -i --tty --image nicolaka/netshoot -- /bin/bash
```

Expected output:
```
If you don't see a command prompt, try pressing enter.
tmp-shell:~# 
```

If we issue a simple `curl` command to one of the pods IPs, we should get a response from the NGINX server:
```
tmp-shell:~# curl 172.16.139.2
<!DOCTYPE html>
<html>
<head>
<title>Welcome to nginx!</title>
<style>
html { color-scheme: light dark; }
body { width: 35em; margin: 0 auto;
font-family: Tahoma, Verdana, Arial, sans-serif; }
</style>
</head>
<body>
<h1>Welcome to nginx!</h1>
<p>If you see this page, the nginx web server is successfully installed and
working. Further configuration is required.</p>

<p>For online documentation and support please refer to
<a href="http://nginx.org/">nginx.org</a>.<br/>
Commercial support is available at
<a href="http://nginx.com/">nginx.com</a>.</p>

<p><em>Thank you for using nginx.</em></p>
</body>
</html>
```
There we go! We have a working EKS cluster, with Calico and two bare-metal nodes. Time to setup urunc! 

### `urunc` setup

The easiest way to setup `urunc` in such a setting is to use `urunc-deploy`. This process follows the principles of `kata-deploy` and is build to work on `k8s` and `k3s`. The process is as follows:

#### 1. Clone the repo

```bash
git clone https://github.com/urunc-dev/urunc
```

#### 2.  Apply the manifests

First we need to create the RBAC
```bash
kubectl apply -f deployment/urunc-deploy/urunc-rbac/urunc-rbac.yaml
```

Then, we create the `urunc-deploy` daemonset:
```bash
kubectl apply -f deployment/urunc-deploy/urunc-deploy/base/urunc-deploy.yaml
```

Finally, we need to create the appropriate k8s runtime class:
```bash
kubectl apply -f deployment/urunc-deploy/runtimeclasses/runtimeclass.yaml
```

Example output:

```
serviceaccount/urunc-deploy-sa created
clusterrole.rbac.authorization.k8s.io/urunc-deploy-role created
clusterrolebinding.rbac.authorization.k8s.io/urunc-deploy-rb created
daemonset.apps/urunc-deploy created
runtimeclass.node.k8s.io/urunc created
```

Monitor the deploy pods once they change their status to `Running`:
```bash
kubectl logs -f -n kube-system -l name=urunc-deploy
```

Example output:
```
Installing qemu
Installing solo5-hvt
Installing solo5-spt
Add urunc as a supported runtime for containerd
Containerd conf file: /etc/containerd/config.toml
Plugin ID: "io.containerd.grpc.v1.cri"
Once again, configuration file is /etc/containerd/config.toml
reloading containerd
node/ip-192-168-103-211.eu-central-1.compute.internal labeled
urunc-deploy completed successfully
Installing qemu
Installing solo5-hvt
Installing solo5-spt
Add urunc as a supported runtime for containerd
Containerd conf file: /etc/containerd/config.toml
Plugin ID: "io.containerd.grpc.v1.cri"
Once again, configuration file is /etc/containerd/config.toml
reloading containerd
node/ip-192-168-32-137.eu-central-1.compute.internal labeled
urunc-deploy completed successfully
```

Now we've got urunc installed on each node, along with the supported hypervisors! Let's try to deploy a unikernel! 

### Run a unikernel
Create a YAML file (e.g. `nginx-urunc.yaml`) with the following contents:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    run: nginx-urunc
  name: nginx-urunc
spec:
  replicas: 1
  selector:
    matchLabels:
      run: nginx-urunc
  template:
    metadata:
      labels:
        run: nginx-urunc
    spec:
      runtimeClassName: urunc
      containers:
      - image: harbor.nbfc.io/nubificus/urunc/nginx-qemu-unikraft-initrd:latest
        imagePullPolicy: Always
        name: nginx-urunc
        command: ["sleep"]
        args: ["infinity"]
        ports:
        - containerPort: 80
          protocol: TCP
        resources:
          requests:
            cpu: 10m
      restartPolicy: Always
---
apiVersion: v1
kind: Service
metadata:
  name: nginx-urunc
spec:
  ports:
  - port: 80
    protocol: TCP
    targetPort: 80
  selector:
    run: nginx-urunc
  sessionAffinity: None
  type: ClusterIP
```
Issuing the command below:
```bash
kubectl apply -f nginx-urunc.yaml
```
will produce the following output:
```
deployment.apps/nginx-urunc created
service/nginx-urunc created
```
and will create a deployment of an NGINX unikernel, from the container image pushed at `harbor.nbfc.io/nubificus/urunc/nginx-hvt-rumprun-block:latest`

Inspecting the pods with `kubectl get pods -o wide` reveals the status:
```
default           nginx-urunc-998b889c4-x798f                1/1     Running             0          2s      172.16.50.225     ip-192-168-32-137.eu-central-1.compute.internal    <none>           <none>
```

and following up on the previous test, we do:

```bash
kubectl run tmp-shell --rm -i --tty --image nicolaka/netshoot -- /bin/bash
```
To get a shell in a pod in the cluster:
```
If you don't see a command prompt, try pressing enter.
tmp-shell:~# 
```

and we `curl` the pod's IP:

```
tmp-shell:~# curl 172.16.50.225
<html>
<body style="font-size: 14pt;">
    <img src="logo150.png"/>
    Served to you by <a href="http://nginx.org/">nginx</a>, running on a
    <a href="http://rumpkernel.org">rump kernel</a>...
</body>
</html>
```

## Conclusions

You now have a fully functional EKS cluster with custom VPC networking and Calico CNI, all set up to run unikernel containers via `urunc`.

We have covered how to:

- Provision foundational infrastructure on AWS  
- Deploy a secure and customizable Kubernetes cluster   
- Configure networking via Calico  
- Prepare node groups with SSH access for hands-on debugging or remote setup
- Install `urunc` via `urunc-deploy`
- Deploy an example unikernel

With your EKS cluster up and running, equipped with Calico networking and ready for `urunc`, you now have a powerful, Kubernetes-native foundation for exploring the next generation of lightweight, secure container runtimes!

