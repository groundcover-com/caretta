Output install_caretta.gif

Set FontSize 32
Set Width 1800
Set Height 600
Set LoopOffset 50%

Type "helm repo add groundcover https://helm.groundcover.com/" Sleep 500ms  Enter
Sleep 500ms
Type "helm repo update" Sleep 500ms  Enter
Sleep 500ms
Type "helm install caretta --namespace caretta --create-namespace groundcover/caretta" Sleep 500ms Enter

Sleep 5s
