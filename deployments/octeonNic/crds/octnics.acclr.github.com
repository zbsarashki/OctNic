apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.11.1
  creationTimestamp: null
  name: octnics.acclr.github.com
spec:
  group: acclr.github.com
  names:
    kind: OctNic
    listKind: OctNicList
    plural: octnics
    singular: octnic
  scope: Namespaced
  versions:
  - name: v1beta1
    schema:
      openAPIV3Schema:
        description: OctNic is the Schema for the octnics API
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: OctNicSpec defines the desired state of OctNic
            properties:
              acclr:
                type: string
              fwimage:
                description: The SDK version refers to runtime FW and Rootfs Versions on the OctNic. This SDK version is used as a TAG to identify the tools update image when updating the device's FW and rootfs
                type: string
              nodename:
                type: string
              numvfs:
                type: string
              pciAddr:
                description: Device configuration
                type: string
              resourceName:
                description: 'Pass resource names and their mappings through CRD Syntax: resourcename: - "marvell_sriov_net_vamp#0" - "marvell_sriov_net_rmp#8-15" - "marvell_sriov_net_dip#20-21" - "marvell_sriov_net_dpp#32,36-37,40-47"'
                items:
                  type: string
                type: array
              resourcePrefix:
                type: string
            type: object
          status:
            description: OctNicStatus defines the observed state of OctNic
            properties:
              OctNicState:
                description: 'INSERT ADDITIONAL STATUS FIELD - define observed state of cluster Important: Run "make" to regenerate code after modifying this file'
                items:
                  properties:
                    nodename:
                      type: string
                    opstate:
                      type: string
                    pciAddr:
                      type: string
                  type: object
                type: array
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}

