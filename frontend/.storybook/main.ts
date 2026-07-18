import type { StorybookConfig } from "@storybook/web-components-vite";

const config: StorybookConfig = {
    framework: "@storybook/web-components-vite",
    stories: ["../src/**/*.stories.@(ts|tsx)"],
    addons: ["@storybook/addon-docs"],
};

export default config;
