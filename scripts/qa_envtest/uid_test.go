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
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestUIDPreconditionAgainstAPIServer(t *testing.T) {
	kubectl := os.Getenv("QA_TEST_KUBECTL")
	if os.Getenv("KUBEBUILDER_ASSETS") == "" || kubectl == "" {
		t.Skip("verified envtest assets and pinned kubectl are required")
	}
	environment := &envtest.Environment{}
	cfg, err := environment.Start()
	if err != nil {
		t.Fatalf("start API server: %v", err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Errorf("stop API server: %v", err)
		}
	})
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatal(err)
	}
	secrets := client.CoreV1().Secrets("default")
	old, err := secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "qa-uid-replacement"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.Delete(ctx, old.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err = secrets.Get(ctx, old.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			break
		}
		if err != nil || time.Now().After(deadline) {
			t.Fatalf("old secret did not disappear: %v", err)
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
	config := clientcmdapi.NewConfig()
	config.Clusters["local"] = &clientcmdapi.Cluster{Server: cfg.Host, CertificateAuthorityData: cfg.CAData}
	config.AuthInfos["local"] = &clientcmdapi.AuthInfo{ClientCertificateData: cfg.CertData, ClientKeyData: cfg.KeyData, Token: cfg.BearerToken}
	config.Contexts["kind-local-dev"] = &clientcmdapi.Context{Cluster: "local", AuthInfo: "local"}
	config.CurrentContext = "kind-local-dev"
	kubeconfig := filepath.Join(t.TempDir(), "config")
	if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
		t.Fatal(err)
	}
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
	runDelete := func(uid string) (string, error) {
		cmd := exec.Command("python3", "-c", program, uid)
		cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "scripts"), "KUBECONFIG="+kubeconfig)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := runDelete(string(old.UID))
	if err == nil || !strings.Contains(strings.ToLower(out), "conflict") {
		t.Fatalf("old UID should receive conflict; err=%v output=%s", err, out)
	}
	remaining, err := secrets.Get(ctx, current.Name, metav1.GetOptions{})
	if err != nil || remaining.UID != current.UID {
		t.Fatalf("replacement was not preserved: %v", err)
	}
	if out, err := runDelete(string(current.UID)); err != nil {
		t.Fatalf("delete current UID: %v: %s", err, out)
	}
	if _, err := secrets.Get(ctx, current.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("current UID deletion not observed: %v", err)
	}
}
