package qa_envtest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	typedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func startServer(t *testing.T) (*rest.Config, *kubernetes.Clientset) {
	t.Helper()
	environment := &envtest.Environment{}
	cfg, err := environment.Start()
	if err != nil {
		t.Fatalf("start API server: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := environment.Stop(); stopErr != nil {
			t.Errorf("stop API server: %v", stopErr)
		}
	})
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatal(err)
	}
	return cfg, client
}

func replaceSecret(t *testing.T, secrets typedcore.SecretInterface) (*corev1.Secret, *corev1.Secret) {
	t.Helper()
	ctx := context.Background()
	old, err := secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "qa-uid-replacement"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deleteErr := secrets.Delete(ctx, old.Name, metav1.DeleteOptions{}); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, getErr := secrets.Get(ctx, old.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			break
		}
		if getErr != nil || time.Now().After(deadline) {
			t.Fatalf("old secret did not disappear: %v", getErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	current, err := secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: old.Name}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if old.UID == current.UID {
		t.Fatal("replacement did not get a new UID")
	}
	return old, current
}

func writeKubeconfig(t *testing.T, cfg *rest.Config) string {
	t.Helper()
	config := clientcmdapi.NewConfig()
	config.Clusters["local"] = &clientcmdapi.Cluster{Server: cfg.Host, CertificateAuthorityData: cfg.CAData}
	config.AuthInfos["local"] = &clientcmdapi.AuthInfo{ClientCertificateData: cfg.CertData, ClientKeyData: cfg.KeyData, Token: cfg.BearerToken}
	config.Contexts["kind-local-dev"] = &clientcmdapi.Context{Cluster: "local", AuthInfo: "local"}
	config.CurrentContext = "kind-local-dev"
	path := filepath.Join(t.TempDir(), "config")
	if writeErr := clientcmd.WriteToFile(*config, path); writeErr != nil {
		t.Fatal(writeErr)
	}
	return path
}

func runDelete(t *testing.T, kubeconfig, uid string) (string, error) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	program := `import os,sys
import qa_live
d=qa_live.Deps(None,None,"123")
c=qa_live.KubectlCluster(os.environ["QA_TEST_KUBECTL"],qa_live.ALLOWED_CONTEXT,{},d)
try:
    c.delete_owned("secret","qa-uid-replacement",sys.argv[1],namespace="default")
except qa_live.SuiteFailure as err:
    print(str(err)); sys.exit(3)
`
	cmd := exec.Command("python3", "-c", program, uid)
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "scripts"), "KUBECONFIG="+kubeconfig)
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr
}

func TestUIDPreconditionAgainstAPIServer(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" || os.Getenv("QA_TEST_KUBECTL") == "" {
		t.Skip("verified envtest assets and pinned kubectl are required")
	}
	cfg, client := startServer(t)
	secrets := client.CoreV1().Secrets("default")
	old, current := replaceSecret(t, secrets)
	kubeconfig := writeKubeconfig(t, cfg)
	out, err := runDelete(t, kubeconfig, string(old.UID))
	if err == nil || !strings.Contains(strings.ToLower(out), "conflict") {
		t.Fatalf("old UID should receive conflict; err=%v output=%s", err, out)
	}
	remaining, err := secrets.Get(context.Background(), current.Name, metav1.GetOptions{})
	if err != nil || remaining.UID != current.UID {
		t.Fatalf("replacement was not preserved: %v", err)
	}
	if output, deleteErr := runDelete(t, kubeconfig, string(current.UID)); deleteErr != nil {
		t.Fatalf("delete current UID: %v: %s", deleteErr, output)
	}
	if _, getErr := secrets.Get(context.Background(), current.Name, metav1.GetOptions{}); !apierrors.IsNotFound(getErr) {
		t.Fatalf("current UID deletion not observed: %v", getErr)
	}
}
