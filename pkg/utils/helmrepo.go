// Copyright 2020 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"fmt"
	"path/filepath"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/repo"
	"k8s.io/klog"

	"github.com/blang/semver"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

func GetPackageAlias(sub *appv1.Subscription, packageName string) string {
	for _, overrides := range sub.Spec.PackageOverrides {
		if overrides.PackageName == packageName {
			klog.Infof("Overrides for package %s found", packageName)

			if overrides.PackageAlias != "" {
				return overrides.PackageAlias
			}
		}
	}

	return ""
}

// GenerateHelmIndexFile generate helm repo index file
func GenerateHelmIndexFile(sub *appv1.Subscription, repoRoot string, chartDirs map[string]string) (*repo.IndexFile, error) {
	// Build a helm repo index file
	indexFile := repo.NewIndexFile()

	for chartDir := range chartDirs {
		chartFolderName := filepath.Base(chartDir)
		chartParentDir := strings.Split(chartDir, chartFolderName)[0]

		// Get the relative parent directory from the git repo root
		chartBaseDir := strings.SplitAfter(chartParentDir, repoRoot+"/")[1]

		chartMetadata, err := chartutil.LoadChartfile(chartDir + "Chart.yaml")

		if err != nil {
			klog.Error("There was a problem in generating helm charts index file: ", err.Error())
			return indexFile, err
		}

		indexFile.Add(chartMetadata, chartFolderName, chartBaseDir, "generated-by-multicloud-operators-subscription")
	}

	indexFile.SortEntries()

	filterCharts(sub, indexFile)

	return indexFile, nil
}

//filterCharts filters the indexFile by name, tillerVersion, version, digest
func filterCharts(sub *appv1.Subscription, indexFile *repo.IndexFile) {
	//Removes all entries from the indexFile with non matching name
	_ = removeNoMatchingName(sub, indexFile)

	//Removes non matching version, tillerVersion, digest
	filterOnVersion(sub, indexFile)
}

//removeNoMatchingName Deletes entries that the name doesn't match the name provided in the subscription
func removeNoMatchingName(sub *appv1.Subscription, indexFile *repo.IndexFile) error {
	if sub.Spec.Package != "" {
		keys := make([]string, 0)
		for k := range indexFile.Entries {
			keys = append(keys, k)
		}

		for _, k := range keys {
			if k != sub.Spec.Package {
				delete(indexFile.Entries, k)
			}
		}
	} else {
		return fmt.Errorf("subsciption.spec.name is missing for subscription: %s/%s", sub.Namespace, sub.Name)
	}

	klog.V(4).Info("After name matching:", indexFile)

	return nil
}

//filterOnVersion filters the indexFile with the version, tillerVersion and Digest provided in the subscription
//The version provided in the subscription can be an expression like ">=1.2.3" (see https://github.com/blang/semver)
//The tillerVersion and the digest provided in the subscription must be literals.
func filterOnVersion(sub *appv1.Subscription, indexFile *repo.IndexFile) {
	keys := make([]string, 0)
	for k := range indexFile.Entries {
		keys = append(keys, k)
	}

	for _, k := range keys {
		chartVersions := indexFile.Entries[k]
		newChartVersions := make([]*repo.ChartVersion, 0)

		for index, chartVersion := range chartVersions {
			if checkKeywords(sub, chartVersion) && checkTillerVersion(sub, chartVersion) && checkVersion(sub, chartVersion) {
				newChartVersions = append(newChartVersions, chartVersions[index])
			}
		}

		if len(newChartVersions) > 0 {
			indexFile.Entries[k] = newChartVersions
		} else {
			delete(indexFile.Entries, k)
		}
	}

	klog.V(4).Info("After version matching:", indexFile)
}

//checkKeywords Checks if the charts has at least 1 keyword from the packageFilter.Keywords array
func checkKeywords(sub *appv1.Subscription, chartVersion *repo.ChartVersion) bool {
	var labelSelector *metav1.LabelSelector
	if sub.Spec.PackageFilter != nil {
		labelSelector = sub.Spec.PackageFilter.LabelSelector
	}

	return KeywordsChecker(labelSelector, chartVersion.Keywords)
}

//checkTillerVersion Checks if the TillerVersion matches
func checkTillerVersion(sub *appv1.Subscription, chartVersion *repo.ChartVersion) bool {
	if sub.Spec.PackageFilter != nil {
		if sub.Spec.PackageFilter.Annotations != nil {
			if filterTillerVersion, ok := sub.Spec.PackageFilter.Annotations["tillerVersion"]; ok {
				tillerVersion := chartVersion.GetTillerVersion()
				if tillerVersion != "" {
					tillerVersionVersion, err := semver.ParseRange(tillerVersion)
					if err != nil {
						klog.Errorf("Error while parsing tillerVersion: %s of %s Error: %s", tillerVersion, chartVersion.GetName(), err.Error())
						return false
					}

					filterTillerVersion, err := semver.Parse(filterTillerVersion)

					if err != nil {
						klog.Error(err)
						return false
					}

					return tillerVersionVersion(filterTillerVersion)
				}
			}
		}
	}

	klog.V(4).Info("Tiller check passed for:", chartVersion)

	return true
}

//checkVersion checks if the version matches
func checkVersion(sub *appv1.Subscription, chartVersion *repo.ChartVersion) bool {
	if sub.Spec.PackageFilter != nil {
		if sub.Spec.PackageFilter.Version != "" {
			version := chartVersion.GetVersion()
			versionVersion, err := semver.Parse(version)

			if err != nil {
				klog.Error(err)
				return false
			}

			filterVersion, err := semver.ParseRange(sub.Spec.PackageFilter.Version)

			if err != nil {
				klog.Error(err)
				return false
			}

			return filterVersion(versionVersion)
		}
	}

	klog.V(4).Info("Version check passed for:", chartVersion)

	return true
}
