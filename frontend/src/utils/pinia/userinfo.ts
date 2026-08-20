import { defineStore } from 'pinia';
const useUserInfoStore = defineStore('userInfo', {
 state: () => ({
   username: '赫赫',
   age: 23,
   like: 'girl',
 }),
 // 其他getters和actions
 persist: true
});
export default useUserInfoStore;