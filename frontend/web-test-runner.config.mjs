import { esbuildPlugin } from "@web/dev-server-esbuild";

/** @type {import("@web/test-runner").TestRunnerConfig} */
export default {
    files: "src/**/*.test.ts",
    nodeResolve: true,
    plugins: [
        esbuildPlugin({
            ts: true,
            target: "auto",
            tsconfig: "tsconfig.json",
        }),
    ],
    coverage: false,
    testFramework: {
        config: { timeout: "2000" },
    },
};
