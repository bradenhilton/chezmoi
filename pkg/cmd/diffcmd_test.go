	"io/fs"
						`    exclude = ["files"]`,
		{
			name: "simple_exclude_externals_with_config",
			extraRoot: map[string]any{
				"/home/user": map[string]any{
					".config/chezmoi/chezmoi.toml": chezmoitest.JoinLines(
						`[diff]`,
						`    exclude = ["externals"]`,
					),
					".local/share/chezmoi": map[string]any{
						"dot_file":            "# contents of .file\n",
						"symlink_dot_symlink": ".file\n",
					},
				},
			},
			stdoutStr: chezmoitest.JoinLines(
				`diff --git a/.file b/.file`,
				`new file mode 100644`,
				`index 0000000000000000000000000000000000000000..8a52cb9ce9551221716a53786ad74104c5902362`,
				`--- /dev/null`,
				`+++ b/.file`,
				`@@ -0,0 +1 @@`,
				`+# contents of .file`,
				`diff --git a/.symlink b/.symlink`,
				`new file mode 120000`,
				`index 0000000000000000000000000000000000000000..3e6844d17780d623d817c3e22bcd1128d64422ae`,
				`--- /dev/null`,
				`+++ b/.symlink`,
				`@@ -0,0 +1 @@`,
				`+.file`,
			),
		},
				"/home/user/.local/share/chezmoi": &vfst.Dir{Perm: fs.ModePerm &^ chezmoitest.Umask},