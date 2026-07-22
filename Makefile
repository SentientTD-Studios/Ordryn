.PHONY: bump-patch bump-minor bump-major

bump-patch:
	./scripts/bump-version.sh patch --commit --tag

bump-minor:
	./scripts/bump-version.sh minor --commit --tag

bump-major:
	./scripts/bump-version.sh major --commit --tag
