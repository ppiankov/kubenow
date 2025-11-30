// Wrapper to build the K8s client.

package util

import (
    "os"
    "path/filepath"

    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/clientcmd"
)

func BuildClient(kubeconfig string) (*kubernetes.Clientset, error) {
    if kubeconfig == "" {
        if env := os.Getenv("KUBECONFIG"); env != "" {
            kubeconfig = env
        } else {
            home, _ := os.UserHomeDir()
            kubeconfig = filepath.Join(home, ".kube", "config")
        }
    }

    cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
    if err != nil {
        return nil, err
    }

    return kubernetes.NewForConfig(cfg)
}
