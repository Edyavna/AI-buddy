import source from '../../../../contracts/examples/scenario.json';

// One canonical dataset. Always clone so one test cannot mutate another's fixtures.
export const createScenario = () => structuredClone(source);
