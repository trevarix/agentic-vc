/** @type {import('jest').Config} */
module.exports = {
  preset: 'ts-jest',
  testEnvironment: 'node',
  rootDir: '.',
  testMatch: ['**/src/__tests__/**/*.test.ts'],
  moduleNameMapper: {
    // Redirect the 'vscode' module to our manual mock.
    '^vscode$': '<rootDir>/__mocks__/vscode.ts',
  },
  transform: {
    '^.+\\.tsx?$': ['ts-jest', {
      tsconfig: {
        // Allow test files outside src/ rootDir.
        rootDir: '.',
        strict: true,
        module: 'commonjs',
        target: 'ES2020',
        lib: ['ES2020'],
        types: ['node', 'jest'],
      },
    }],
  },
  collectCoverageFrom: [
    'src/**/*.ts',
    '!src/**/*.d.ts',
    '!src/extension.ts',
  ],
};
