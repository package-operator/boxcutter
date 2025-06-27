Conflicts:
- \"Hans\"
  .spec.replicas
Other:
- \"Franz\"
  .metadata.annotations.test
- \"kube-controller-manager\"
  .metadata.annotations
  .metadata.annotations.deployment.kubernetes.io/revision
  .status.conditions
  .status.observedGeneration
  .status.replicas
  .status.unavailableReplicas
  .status.updatedReplicas
  .status.conditions[type=\"Available\"]
  .status.conditions[type=\"Progressing\"]
  .status.conditions[type=\"Available\"].lastTransitionTime
  .status.conditions[type=\"Available\"].lastUpdateTime
  .status.conditions[type=\"Available\"].message
  .status.conditions[type=\"Available\"].reason
  .status.conditions[type=\"Available\"].status
  .status.conditions[type=\"Available\"].type
  .status.conditions[type=\"Progressing\"].lastTransitionTime
  .status.conditions[type=\"Progressing\"].lastUpdateTime
  .status.conditions[type=\"Progressing\"].message
  .status.conditions[type=\"Progressing\"].reason
  .status.conditions[type=\"P
  rogressing\"].status
  .status.conditions[type=\"Progressing\"].type
Comparison:
- Added:
  .metadata.annotations.deployment.kubernetes.io/revision
  .metadata.annotations.test
  .spec.progressDeadlineSeconds
  .spec.revisionHistoryLimit
  .spec.strategy.type
  .spec.strategy.rollingUpdate.maxSurge
  .spec.strategy.rollingUpdate.maxUnavailable
  .spec.template.spec.dnsPolicy
  .spec.template.spec.restartPolicy
  .spec.template.spec.schedulerName
  .spec.template.spec.securityContext
  .spec.template.spec.terminationGracePeriodSeconds
  .spec.template.spec.containers[name=\"app\"].imagePullPolicy
  .spec.template.spec.containers[name=\"app\"].terminationMessagePath
  .spec.template.spec.containers[name=\"app\"].terminationMessagePolicy
  .status.observedGeneration
  .status.replicas
  .status.unavailableReplicas
  .status.updatedReplicas
  .status.conditions[type=\"Available\"].lastTransitionTime
  .status.conditions[type=\"Available\"].lastUpdateTime
  .status.conditions[type=\"Availa
  ble\"].message
  .status.conditions[type=\"Available\"].reason
  .status.conditions[type=\"Available\"].status
  .status.conditions[type=\"Available\"].type
  .status.conditions[type=\"Progressing\"].lastTransitionTime
  .status.conditions[type=\"Progressing\"].lastUpdateTime
  .status.conditions[type=\"Progressing\"].message
  .status.conditions[type=\"Progressing\"].reason
  .status.conditions[type=\"Progressing\"].status
  .status.conditions[type=\"Progressing\"].type
- Modified:
  .spec.replicas
  .spec.template.spec.containers[name=\"app\"].image

