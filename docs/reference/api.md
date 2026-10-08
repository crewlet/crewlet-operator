# API Reference

## Packages
- [crewlet.ai/v1alpha1](#crewletaiv1alpha1)


## crewlet.ai/v1alpha1

Package v1alpha1 contains the v1alpha1 API of the crewlet.ai group: a
Crewlet company and its org chart as Kubernetes objects.


### Resource Types
- [Company](#company)
- [EngineClass](#engineclass)
- [Integration](#integration)
- [LLMProvider](#llmprovider)
- [MCPServer](#mcpserver)
- [Seat](#seat)
- [Unit](#unit)



#### Company



Company is the Schema for the companies API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `Company` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[CompanySpec](#companyspec)_ | spec defines the desired state of Company |  | Required: \{\} <br /> |
| `status` _[CompanyStatus](#companystatus)_ | status defines the observed state of Company |  | Optional: \{\} <br /> |


#### CompanySpec



CompanySpec defines the desired state of Company



_Appears in:_
- [Company](#company)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | foo is an example field of Company. Edit company_types.go to remove/update |  | Optional: \{\} <br /> |


#### CompanyStatus



CompanyStatus defines the observed state of Company.



_Appears in:_
- [Company](#company)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions represent the current state of the Company resource.<br />Each condition has a unique type and reflects the status of a specific aspect of the resource.<br />Standard condition types include:<br />- "Available": the resource is fully functional<br />- "Progressing": the resource is being created or updated<br />- "Degraded": the resource failed to reach or maintain its desired state<br />The status of each condition is one of True, False, or Unknown. |  | Optional: \{\} <br /> |


#### EngineClass



EngineClass is the Schema for the engineclasses API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `EngineClass` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[EngineClassSpec](#engineclassspec)_ | spec defines the desired state of EngineClass |  | Required: \{\} <br /> |
| `status` _[EngineClassStatus](#engineclassstatus)_ | status defines the observed state of EngineClass |  | Optional: \{\} <br /> |


#### EngineClassSpec



EngineClassSpec defines the desired state of EngineClass



_Appears in:_
- [EngineClass](#engineclass)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | foo is an example field of EngineClass. Edit engineclass_types.go to remove/update |  | Optional: \{\} <br /> |


#### EngineClassStatus



EngineClassStatus defines the observed state of EngineClass.



_Appears in:_
- [EngineClass](#engineclass)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions represent the current state of the EngineClass resource.<br />Each condition has a unique type and reflects the status of a specific aspect of the resource.<br />Standard condition types include:<br />- "Available": the resource is fully functional<br />- "Progressing": the resource is being created or updated<br />- "Degraded": the resource failed to reach or maintain its desired state<br />The status of each condition is one of True, False, or Unknown. |  | Optional: \{\} <br /> |


#### Integration



Integration is the Schema for the integrations API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `Integration` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[IntegrationSpec](#integrationspec)_ | spec defines the desired state of Integration |  | Required: \{\} <br /> |
| `status` _[IntegrationStatus](#integrationstatus)_ | status defines the observed state of Integration |  | Optional: \{\} <br /> |


#### IntegrationSpec



IntegrationSpec defines the desired state of Integration



_Appears in:_
- [Integration](#integration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | foo is an example field of Integration. Edit integration_types.go to remove/update |  | Optional: \{\} <br /> |


#### IntegrationStatus



IntegrationStatus defines the observed state of Integration.



_Appears in:_
- [Integration](#integration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions represent the current state of the Integration resource.<br />Each condition has a unique type and reflects the status of a specific aspect of the resource.<br />Standard condition types include:<br />- "Available": the resource is fully functional<br />- "Progressing": the resource is being created or updated<br />- "Degraded": the resource failed to reach or maintain its desired state<br />The status of each condition is one of True, False, or Unknown. |  | Optional: \{\} <br /> |


#### LLMProvider



LLMProvider is the Schema for the llmproviders API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `LLMProvider` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[LLMProviderSpec](#llmproviderspec)_ | spec defines the desired state of LLMProvider |  | Required: \{\} <br /> |
| `status` _[LLMProviderStatus](#llmproviderstatus)_ | status defines the observed state of LLMProvider |  | Optional: \{\} <br /> |


#### LLMProviderSpec



LLMProviderSpec defines the desired state of LLMProvider



_Appears in:_
- [LLMProvider](#llmprovider)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | foo is an example field of LLMProvider. Edit llmprovider_types.go to remove/update |  | Optional: \{\} <br /> |


#### LLMProviderStatus



LLMProviderStatus defines the observed state of LLMProvider.



_Appears in:_
- [LLMProvider](#llmprovider)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions represent the current state of the LLMProvider resource.<br />Each condition has a unique type and reflects the status of a specific aspect of the resource.<br />Standard condition types include:<br />- "Available": the resource is fully functional<br />- "Progressing": the resource is being created or updated<br />- "Degraded": the resource failed to reach or maintain its desired state<br />The status of each condition is one of True, False, or Unknown. |  | Optional: \{\} <br /> |


#### MCPServer



MCPServer is the Schema for the mcpservers API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `MCPServer` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[MCPServerSpec](#mcpserverspec)_ | spec defines the desired state of MCPServer |  | Required: \{\} <br /> |
| `status` _[MCPServerStatus](#mcpserverstatus)_ | status defines the observed state of MCPServer |  | Optional: \{\} <br /> |


#### MCPServerSpec



MCPServerSpec defines the desired state of MCPServer



_Appears in:_
- [MCPServer](#mcpserver)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | foo is an example field of MCPServer. Edit mcpserver_types.go to remove/update |  | Optional: \{\} <br /> |


#### MCPServerStatus



MCPServerStatus defines the observed state of MCPServer.



_Appears in:_
- [MCPServer](#mcpserver)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions represent the current state of the MCPServer resource.<br />Each condition has a unique type and reflects the status of a specific aspect of the resource.<br />Standard condition types include:<br />- "Available": the resource is fully functional<br />- "Progressing": the resource is being created or updated<br />- "Degraded": the resource failed to reach or maintain its desired state<br />The status of each condition is one of True, False, or Unknown. |  | Optional: \{\} <br /> |


#### Seat



Seat is the Schema for the seats API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `Seat` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[SeatSpec](#seatspec)_ | spec defines the desired state of Seat |  | Required: \{\} <br /> |
| `status` _[SeatStatus](#seatstatus)_ | status defines the observed state of Seat |  | Optional: \{\} <br /> |


#### SeatSpec



SeatSpec defines the desired state of Seat



_Appears in:_
- [Seat](#seat)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | foo is an example field of Seat. Edit seat_types.go to remove/update |  | Optional: \{\} <br /> |


#### SeatStatus



SeatStatus defines the observed state of Seat.



_Appears in:_
- [Seat](#seat)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions represent the current state of the Seat resource.<br />Each condition has a unique type and reflects the status of a specific aspect of the resource.<br />Standard condition types include:<br />- "Available": the resource is fully functional<br />- "Progressing": the resource is being created or updated<br />- "Degraded": the resource failed to reach or maintain its desired state<br />The status of each condition is one of True, False, or Unknown. |  | Optional: \{\} <br /> |


#### Unit



Unit is the Schema for the units API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `Unit` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[UnitSpec](#unitspec)_ | spec defines the desired state of Unit |  | Required: \{\} <br /> |
| `status` _[UnitStatus](#unitstatus)_ | status defines the observed state of Unit |  | Optional: \{\} <br /> |


#### UnitSpec



UnitSpec defines the desired state of Unit



_Appears in:_
- [Unit](#unit)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | foo is an example field of Unit. Edit unit_types.go to remove/update |  | Optional: \{\} <br /> |


#### UnitStatus



UnitStatus defines the observed state of Unit.



_Appears in:_
- [Unit](#unit)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions represent the current state of the Unit resource.<br />Each condition has a unique type and reflects the status of a specific aspect of the resource.<br />Standard condition types include:<br />- "Available": the resource is fully functional<br />- "Progressing": the resource is being created or updated<br />- "Degraded": the resource failed to reach or maintain its desired state<br />The status of each condition is one of True, False, or Unknown. |  | Optional: \{\} <br /> |


